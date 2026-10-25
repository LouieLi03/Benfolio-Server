package workingpdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/resource"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/color"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func render(ctx context.Context, userID string, source domain.OriginalFile, snapshot domain.WorkingPDFSnapshot,
	resources domain.ResourceService, output io.Writer) (count int, err error) {
	defer func() {
		if recover() != nil {
			count = 0
			err = domain.ErrWorkingPDFRender
		}
	}()
	if source.Version.ByteSize > 32<<20 {
		return 0, domain.ErrWorkingPDFLimit
	}
	conf := model.NewStatelessConfiguration()
	conf.Cmd, conf.ValidationMode = model.ADDANNOTATIONS, model.ValidationRelaxed
	conf.Offline, conf.ValidateLinks, conf.Optimize, conf.OptimizeBeforeWriting = true, false, false, false
	conf.LocalImageRoot, conf.DecodeAllStreams, conf.PreserveInfoDict = "", false, true
	conf.Limits = model.ResourceLimits{MaxInputBytes: 32 << 20, MaxObjectBytes: 4 << 20, MaxStreamBytes: 32 << 20,
		MaxDecodeBytes: 32 << 20, MaxImagePixels: 16_000_000, MaxImageBytes: 64 << 20,
		MaxObjectCount: 100_000, MaxObjectStreamCount: 10_000, MaxObjectStreamFirst: 1 << 20,
		MaxXRefEntries: 100_000, MaxRecursionDepth: 64}
	pdf, err := api.ReadAndValidate(ctx, source.Content, conf)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, domain.ErrWorkingPDFRender
	}
	if pdf == nil || pdf.XRefTable == nil || pdf.Encrypt != nil || pdf.SignatureExist || pdf.AppendOnly {
		return 0, domain.ErrWorkingPDFUnsupported
	}
	if pdf.PageCount < 1 || pdf.PageCount > 1000 || pdf.PageCount != snapshot.PageCount {
		return 0, domain.ErrWorkingPDFLimit
	}
	annotations := map[int][]model.AnnotationRenderer{}
	var points, strokes, objectCount int
	var resourceBytes int64
	for _, note := range snapshot.Annotations {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if note.Kind == "bookmark" {
			continue
		}
		if note.PDF == nil || note.PDF.PageIndex == nil || *note.PDF.PageIndex < 0 || *note.PDF.PageIndex >= int64(pdf.PageCount) {
			return 0, domain.ErrWorkingPDFUnsupported
		}
		page := int(*note.PDF.PageIndex) + 1
		crop, err := pageCrop(ctx, pdf, page)
		if err != nil {
			return 0, err
		}
		var body struct {
			Text           string `json:"text"`
			Color          string `json:"color"`
			ResourceSHA256 string `json:"resourceSHA256"`
		}
		if json.Unmarshal(note.Body, &body) != nil {
			return 0, domain.ErrWorkingPDFRender
		}
		name := func(suffix string) string {
			sum := sha256.Sum256([]byte(userID + "\n" + note.DocumentVersionID + "\n" + note.ID + "\n" + suffix))
			return "benfolio:" + hex.EncodeToString(sum[:])
		}
		switch note.Kind {
		case "highlight":
			col, _, err := parseColor(body.Color, false)
			if err != nil {
				return 0, err
			}
			if len(note.PDF.Rects) == 0 {
				return 0, domain.ErrWorkingPDFUnsupported
			}
			for i, rect := range note.PDF.Rects {
				r, err := normalizedRect(crop, rect)
				if err != nil {
					return 0, err
				}
				annotations[page] = append(annotations[page], pdfAnnotation{Annotation: baseAnnotation(model.AnnHighLight, r, body.Text, name(fmt.Sprint(i)), col, note),
					alpha: 0.35, multiply: true, highlight: true})
			}
		case "note":
			x, y := crop.LL.X, crop.UR.Y
			if len(note.PDF.Rects) > 0 {
				target, err := normalizedRect(crop, note.PDF.Rects[0])
				if err != nil {
					return 0, err
				}
				x, y = target.LL.X, target.UR.Y
			}
			width, height := math.Min(20, crop.Width()), math.Min(20, crop.Height())
			x = math.Min(x, crop.UR.X-width)
			y = math.Max(y, crop.LL.Y+height)
			r := *types.RectForWidthAndHeight(x, y-height, width, height)
			col := color.SimpleColor{R: 1, G: 0.9, B: 0.2}
			annotations[page] = append(annotations[page], pdfAnnotation{Annotation: baseAnnotation(model.AnnText, r, body.Text, name("note"), col, note), alpha: 1, textNote: true})
		case "ink":
			file, err := resources.Open(ctx, userID, body.ResourceSHA256)
			if err != nil {
				return 0, err
			}
			resourceBytes += file.Resource.ByteSize
			if resourceBytes > 16<<20 {
				_ = file.Content.Close()
				return 0, domain.ErrWorkingPDFLimit
			}
			data, readErr := io.ReadAll(io.LimitReader(file.Content, domain.MaxInkBytes+1))
			closeErr := file.Content.Close()
			if readErr != nil {
				return 0, readErr
			}
			if closeErr != nil {
				return 0, closeErr
			}
			ink, err := resource.DecodeInk(ctx, data)
			if err != nil {
				return 0, err
			}
			if ink.DocumentVersionID != note.DocumentVersionID || ink.SourceSHA256 != source.Version.SHA256 || ink.PageIndex == nil || *ink.PageIndex != *note.PDF.PageIndex {
				return 0, domain.ErrResourceBinding
			}
			for _, stroke := range ink.Strokes {
				strokes++
				points += len(stroke.Points)
				if strokes > 20000 || points > 200000 {
					return 0, domain.ErrWorkingPDFLimit
				}
				if err := ctx.Err(); err != nil {
					return 0, err
				}
				col, alpha, err := parseColor(stroke.Color, true)
				if err != nil {
					return 0, err
				}
				width := stroke.Width * crop.Width()
				if width < 0.01 {
					return 0, domain.ErrWorkingPDFUnsupported
				}
				path := make(model.InkPath, 0, len(stroke.Points)*2)
				minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for _, point := range stroke.Points {
					x, y := crop.LL.X+point.X*crop.Width(), crop.UR.Y-point.Y*crop.Height()
					path = append(path, x, y)
					minX = math.Min(minX, x)
					minY = math.Min(minY, y)
					maxX = math.Max(maxX, x)
					maxY = math.Max(maxY, y)
				}
				r := *types.RectForWidthAndHeight(minX-width/2, minY-width/2, maxX-minX+width, maxY-minY+width)
				annotations[page] = append(annotations[page], pdfAnnotation{Annotation: baseAnnotation(model.AnnInk, r, "", name(stroke.ID), col, note),
					alpha: alpha, multiply: stroke.Tool == "highlighter", ink: path, width: width})
			}
		default:
			return 0, domain.ErrWorkingPDFUnsupported
		}
		// Each source annotation may expand into several PDF annotation objects.
		objectCount = 0
		for _, items := range annotations {
			objectCount += len(items)
		}
		if objectCount > 20000 {
			return 0, domain.ErrWorkingPDFLimit
		}
		count++
	}
	if len(annotations) == 0 {
		if _, err := source.Content.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		_, err := io.Copy(output, source.Content)
		return count, err
	}
	pdf.EnsureVersionForWriting()
	if added, err := pdfcpu.AddAnnotationsMap(ctx, pdf, annotations, false); err != nil || !added {
		return 0, domain.ErrWorkingPDFRender
	}
	if err := api.ValidateContext(ctx, pdf); err != nil {
		return 0, domain.ErrWorkingPDFRender
	}
	if err := api.WriteContext(ctx, pdf, output); err != nil {
		return 0, err
	}
	return count, ctx.Err()
}

func baseAnnotation(kind model.AnnotationType, rect types.Rectangle, text, id string, col color.SimpleColor, note domain.Annotation) model.Annotation {
	return model.Annotation{SubType: kind, Rect: rect, Contents: text, NM: id, ModificationDate: types.DateString(note.UpdatedAt), F: model.AnnPrint, C: &col}
}

func pageCrop(ctx context.Context, pdf *model.Context, page int) (types.Rectangle, error) {
	_, _, attrs, err := pdf.PageDict(ctx, page, false)
	if err != nil || attrs == nil || attrs.MediaBox == nil || attrs.Rotate%90 != 0 {
		return types.Rectangle{}, domain.ErrWorkingPDFUnsupported
	}
	box := attrs.CropBox
	if box == nil {
		box = attrs.MediaBox
	}
	if !finite(box.LL.X, box.LL.Y, box.UR.X, box.UR.Y, attrs.MediaBox.LL.X, attrs.MediaBox.LL.Y, attrs.MediaBox.UR.X, attrs.MediaBox.UR.Y) || box.Width() <= 0 || box.Height() <= 0 || box.Width() > 20000 || box.Height() > 20000 ||
		box.LL.X < attrs.MediaBox.LL.X || box.LL.Y < attrs.MediaBox.LL.Y || box.UR.X > attrs.MediaBox.UR.X || box.UR.Y > attrs.MediaBox.UR.Y {
		return types.Rectangle{}, domain.ErrWorkingPDFUnsupported
	}
	return *box, nil
}

func normalizedRect(crop types.Rectangle, rect domain.Rect) (types.Rectangle, error) {
	if !finite(rect.X, rect.Y, rect.Width, rect.Height) || rect.X < 0 || rect.Y < 0 || rect.Width <= 0 || rect.Height <= 0 || rect.X+rect.Width > 1 || rect.Y+rect.Height > 1 {
		return types.Rectangle{}, domain.ErrWorkingPDFUnsupported
	}
	result := *types.RectForWidthAndHeight(crop.LL.X+rect.X*crop.Width(), crop.UR.Y-(rect.Y+rect.Height)*crop.Height(), rect.Width*crop.Width(), rect.Height*crop.Height())
	if result.Width() < 0.000001 || result.Height() < 0.000001 {
		return types.Rectangle{}, domain.ErrWorkingPDFUnsupported
	}
	return result, nil
}

func parseColor(value string, rgba bool) (color.SimpleColor, float64, error) {
	if value == "" && !rgba {
		value = "#FFF176"
	}
	expected := 7
	if rgba {
		expected = 9
	}
	if len(value) != expected || value[0] != '#' {
		return color.SimpleColor{}, 0, domain.ErrWorkingPDFUnsupported
	}
	data, err := hex.DecodeString(value[1:])
	if err != nil {
		return color.SimpleColor{}, 0, domain.ErrWorkingPDFUnsupported
	}
	col := color.SimpleColor{R: float32(data[0]) / 255, G: float32(data[1]) / 255, B: float32(data[2]) / 255}
	alpha := 1.0
	if rgba {
		alpha = float64(data[3]) / 255
	}
	return col, alpha, nil
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// pdfAnnotation supplies an explicit appearance as well as standard annotation
// geometry. Stroke pressure/timing remains authoritative in the ink resource.
type pdfAnnotation struct {
	model.Annotation
	alpha                         float64
	multiply, highlight, textNote bool
	ink                           model.InkPath
	width                         float64
}

func (ann pdfAnnotation) RenderDict(ctx context.Context, xref *model.XRefTable, page *types.IndirectRef) (types.Dict, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, err := ann.Annotation.RenderDict(ctx, xref, page)
	if err != nil {
		return nil, err
	}
	d["M"] = d["ModDate"]
	delete(d, "ModDate")
	d["T"] = types.StringLiteral("Benfolio")
	d["CA"] = types.Float(1)
	var stream bytes.Buffer
	fmt.Fprintf(&stream, "q\n/GS0 gs\n1 0 0 1 %.8f %.8f cm\n%.8f %.8f %.8f RG\n%.8f %.8f %.8f rg\n",
		-ann.Rect.LL.X, -ann.Rect.LL.Y, ann.C.R, ann.C.G, ann.C.B, ann.C.R, ann.C.G, ann.C.B)
	if len(ann.ink) != 0 {
		d["InkList"] = types.Array{types.NewNumberArray(ann.ink...)}
		d["BS"] = types.Dict{"W": types.Float(ann.width), "S": types.Name("S")}
		fmt.Fprintf(&stream, "%.8f w 1 J 1 j\n", ann.width)
		if len(ann.ink) == 2 {
			x, y, r := ann.ink[0], ann.ink[1], ann.width/2
			k := r * 0.5522847498
			fmt.Fprintf(&stream, "%.8f %.8f m\n", x+r, y)
			for _, v := range [][6]float64{{x + r, y + k, x + k, y + r, x, y + r}, {x - k, y + r, x - r, y + k, x - r, y}, {x - r, y - k, x - k, y - r, x, y - r}, {x + k, y - r, x + r, y - k, x + r, y}} {
				fmt.Fprintf(&stream, "%.8f %.8f %.8f %.8f %.8f %.8f c\n", v[0], v[1], v[2], v[3], v[4], v[5])
			}
			stream.WriteString("f\n")
		} else {
			fmt.Fprintf(&stream, "%.8f %.8f m\n", ann.ink[0], ann.ink[1])
			for i := 2; i < len(ann.ink); i += 2 {
				if i%256 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				fmt.Fprintf(&stream, "%.8f %.8f l\n", ann.ink[i], ann.ink[i+1])
			}
			stream.WriteString("S\n")
		}
	} else {
		fmt.Fprintf(&stream, "%.8f %.8f %.8f %.8f re f\n", ann.Rect.LL.X, ann.Rect.LL.Y, ann.Rect.Width(), ann.Rect.Height())
		if ann.highlight {
			d["QuadPoints"] = types.NewNumberArray(ann.Rect.LL.X, ann.Rect.UR.Y, ann.Rect.UR.X, ann.Rect.UR.Y, ann.Rect.LL.X, ann.Rect.LL.Y, ann.Rect.UR.X, ann.Rect.LL.Y)
		}
		if ann.textNote {
			d["Name"], d["Open"] = types.Name("Note"), types.Boolean(false)
			stream.WriteString("0 0 0 RG 0.5 w\n")
			for _, f := range []float64{0.3, 0.5, 0.7} {
				y := ann.Rect.LL.Y + ann.Rect.Height()*f
				fmt.Fprintf(&stream, "%.8f %.8f m %.8f %.8f l S\n", ann.Rect.LL.X+ann.Rect.Width()*0.2, y, ann.Rect.LL.X+ann.Rect.Width()*0.8, y)
			}
		}
	}
	stream.WriteString("Q\n")
	sd, err := xref.NewStreamDictForBuf(stream.Bytes())
	if err != nil {
		return nil, err
	}
	sd.Dict["Type"], sd.Dict["Subtype"], sd.Dict["FormType"] = types.Name("XObject"), types.Name("Form"), types.Integer(1)
	sd.Dict["BBox"] = types.NewNumberArray(0, 0, ann.Rect.Width(), ann.Rect.Height())
	blend := "Normal"
	if ann.multiply {
		blend = "Multiply"
	}
	sd.Dict["Resources"] = types.Dict{"ExtGState": types.Dict{"GS0": types.Dict{"Type": types.Name("ExtGState"), "CA": types.Float(ann.alpha), "ca": types.Float(ann.alpha), "BM": types.Name(blend)}}}
	if err := sd.Encode(); err != nil {
		return nil, err
	}
	ref, err := xref.IndRefForNewObject(*sd)
	if err != nil {
		return nil, err
	}
	d["AP"] = types.Dict{"N": *ref}
	return d, nil
}
