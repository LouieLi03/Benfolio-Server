package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

type markdownBuffer struct {
	bytes.Buffer
	err error
}

func (b *markdownBuffer) line(format string, args ...any) {
	if b.err != nil {
		return
	}
	text := fmt.Sprintf(format, args...) + "\n"
	if int64(b.Len()+len(text)) > domain.MaxMarkdownBytes {
		b.err = domain.ErrExportLimit
		return
	}
	_, _ = b.WriteString(text)
}

var markdownEscape = strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "!", "\\!", "#", "\\#", ">", "\\>", "|", "\\|", "~", "\\~")

func literal(text string) string { return markdownEscape.Replace(html.EscapeString(text)) }
func oneLine(text string) string { return literal(strings.Join(strings.Fields(text), " ")) }
func quote(b *markdownBuffer, text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(text, "\n") {
		b.line("> %s", literal(line))
	}
	b.line("")
}

func markdown(ctx context.Context, snapshot domain.ExportSnapshot, created time.Time) ([]byte, error) {
	var b markdownBuffer
	b.line("# %s", oneLine(snapshot.Title))
	b.line("")
	b.line("- 作者：%s", oneLine(strings.Join(snapshot.Authors, "、")))
	b.line("- 书籍格式：%s", strings.ToUpper(string(snapshot.BookFormat)))
	b.line("- 原件版本：%s", snapshot.DocumentVersionID)
	b.line("- 原件摘要：%s", snapshot.SourceSHA256)
	b.line("- 批注快照：%d", snapshot.AnnotationSeq)
	b.line("- 导出时间：%s", created.UTC().Format(time.RFC3339Nano))
	b.line("")
	b.line("> 这是可读笔记汇总，不包含原书、笔画文件、删除记录或阅读进度，不能代替完整恢复备份。")
	b.line("")
	if len(snapshot.Annotations) == 0 {
		b.line("此快照没有有效批注。")
		return b.Bytes(), b.err
	}
	labels := map[string]string{"bookmark": "书签", "highlight": "高亮", "note": "文字笔记", "ink": "手写"}
	for i, note := range snapshot.Annotations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		label, ok := labels[note.Kind]
		if !ok {
			return nil, domain.ErrInvalidExport
		}
		if note.Kind == "ink" && snapshot.BookFormat != domain.PDF {
			return nil, domain.ErrResourceConflict
		}
		var body struct {
			Text           string `json:"text"`
			Color          string `json:"color"`
			ResourceSHA256 string `json:"resourceSHA256"`
		}
		if json.Unmarshal(note.Body, &body) != nil {
			return nil, domain.ErrResourceConflict
		}
		b.line("## %d. %s", i+1, label)
		b.line("")
		b.line("- 批注ID：%s", oneLine(note.ID))
		b.line("- 修订：%d", note.Revision)
		b.line("- 创建时间：%s", note.CreatedAt.UTC().Format(time.RFC3339Nano))
		b.line("- 修改时间：%s", note.UpdatedAt.UTC().Format(time.RFC3339Nano))
		var excerpt string
		switch snapshot.BookFormat {
		case domain.EPUB:
			if note.EPUB == nil || note.PDF != nil {
				return nil, domain.ErrResourceConflict
			}
			b.line("- 内容位置：%s", oneLine(note.EPUB.Href))
			if note.EPUB.CFI != "" {
				b.line("- CFI：%s", oneLine(note.EPUB.CFI))
			}
			excerpt = note.EPUB.Quote.Exact
		case domain.PDF:
			if note.PDF == nil || note.EPUB != nil || note.PDF.PageIndex == nil || *note.PDF.PageIndex < 0 || *note.PDF.PageIndex >= int64(snapshot.PageCount) {
				return nil, domain.ErrResourceConflict
			}
			b.line("- 位置：原件第 %d 页（零基索引 %d）", *note.PDF.PageIndex+1, *note.PDF.PageIndex)
			if note.PDF.Quote != nil {
				excerpt = note.PDF.Quote.Exact
			}
		default:
			return nil, domain.ErrInvalidExport
		}
		if body.Color != "" {
			b.line("- 高亮颜色：%s", oneLine(body.Color))
		}
		b.line("")
		if excerpt != "" {
			b.line("### 摘录")
			b.line("")
			quote(&b, excerpt)
		}
		if body.Text != "" {
			b.line("### 备注")
			b.line("")
			quote(&b, body.Text)
		}
		if note.Kind == "ink" {
			if !syncvalue.ValidSHA256(body.ResourceSHA256) {
				return nil, domain.ErrResourceConflict
			}
			b.line("- 手写资源摘要：%s", body.ResourceSHA256)
			b.line("- 本汇总未包含笔画字节，请通过阅读器或资源接口另行取得。")
			b.line("")
		}
		if b.err != nil {
			return nil, b.err
		}
	}
	return b.Bytes(), b.err
}
