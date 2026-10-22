// Package publication reads bounded publication metadata without extracting
// archive members, fetching network resources or executing document content.
package publication

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"io"
	"io/fs"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

const (
	MaxEPUBBytes      int64 = 128 << 20
	maxZIPEntries           = 10_000
	maxDirectoryBytes       = 8 << 20
	maxXMLBytes             = 2 << 20
	opfNS                   = "http://www.idpf.org/2007/opf"
)

type containerDocument struct {
	XMLName   xml.Name `xml:"urn:oasis:names:tc:opendocument:xmlns:container container"`
	Rootfiles struct {
		Items []struct {
			Path      string `xml:"full-path,attr"`
			MediaType string `xml:"media-type,attr"`
		} `xml:"urn:oasis:names:tc:opendocument:xmlns:container rootfile"`
	} `xml:"urn:oasis:names:tc:opendocument:xmlns:container rootfiles"`
}

type packageDocument struct {
	XMLName  xml.Name `xml:"http://www.idpf.org/2007/opf package"`
	Version  string   `xml:"version,attr"`
	Metadata struct {
		Titles      []string `xml:"http://purl.org/dc/elements/1.1/ title"`
		Creators    []string `xml:"http://purl.org/dc/elements/1.1/ creator"`
		Languages   []string `xml:"http://purl.org/dc/elements/1.1/ language"`
		Identifiers []string `xml:"http://purl.org/dc/elements/1.1/ identifier"`
	} `xml:"http://www.idpf.org/2007/opf metadata"`
	Manifest struct {
		Items []struct {
			ID        string `xml:"id,attr"`
			Href      string `xml:"href,attr"`
			MediaType string `xml:"media-type,attr"`
		} `xml:"http://www.idpf.org/2007/opf item"`
	} `xml:"http://www.idpf.org/2007/opf manifest"`
	Spine struct {
		Items []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"http://www.idpf.org/2007/opf itemref"`
	} `xml:"http://www.idpf.org/2007/opf spine"`
}

func ReadEPUB(ctx context.Context, data io.ReaderAt, size int64) (domain.EPUBMetadata, error) {
	if size <= 0 || size > MaxEPUBBytes {
		return domain.EPUBMetadata{}, domain.ErrImportSize
	}
	if err := ctx.Err(); err != nil {
		return domain.EPUBMetadata{}, err
	}
	count, err := boundedDirectory(data, size)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	z, err := zip.NewReader(data, size)
	if err != nil || len(z.File) != count {
		return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
	}
	files := make(map[string]*zip.File, len(z.File))
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !utf8.ValidString(name) || !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\\x00") || files[name] != nil {
			return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
		}
		files[name] = f
	}
	mimetype := files["mimetype"]
	if mimetype == nil || mimetype.Method != zip.Store {
		return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
	}
	value, err := readMember(ctx, mimetype, 20)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	if string(value) != "application/epub+zip" {
		return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
	}
	containerBytes, err := readMember(ctx, files["META-INF/container.xml"], 1<<20)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	var container containerDocument
	if err := decodeXML(ctx, containerBytes, &container); err != nil {
		return domain.EPUBMetadata{}, err
	}
	var packagePath string
	for _, item := range container.Rootfiles.Items {
		if item.MediaType == "application/oebps-package+xml" {
			packagePath, err = memberPath(".", item.Path)
			if err != nil {
				return domain.EPUBMetadata{}, err
			}
			break
		}
	}
	opfBytes, err := readMember(ctx, files[packagePath], maxXMLBytes)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	var pkg packageDocument
	if err := decodeXML(ctx, opfBytes, &pkg); err != nil {
		return domain.EPUBMetadata{}, err
	}
	if pkg.XMLName.Space != opfNS || (pkg.Version != "2.0" && pkg.Version != "3.0") {
		return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
	}
	titles, err := metadataValues(pkg.Metadata.Titles, 64)
	if err != nil || len(titles) == 0 {
		return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
	}
	authors, err := metadataValues(pkg.Metadata.Creators, 64)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	identifiers, err := metadataValues(pkg.Metadata.Identifiers, 64)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	languages, err := metadataValues(pkg.Metadata.Languages, 64)
	if err != nil {
		return domain.EPUBMetadata{}, err
	}
	language := ""
	if len(languages) > 0 {
		language = languages[0]
	}
	// Verify local spine references, without treating this as a full EPUB or
	// rendering validator. Remote resources are never requested.
	items := make(map[string]string, len(pkg.Manifest.Items))
	for _, item := range pkg.Manifest.Items {
		_, duplicate := items[item.ID]
		if item.ID == "" || item.Href == "" || duplicate {
			return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
		}
		items[item.ID] = item.Href
	}
	if len(pkg.Spine.Items) == 0 {
		return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
	}
	for _, item := range pkg.Spine.Items {
		name, err := memberPath(path.Dir(packagePath), items[item.IDRef])
		if err != nil {
			return domain.EPUBMetadata{}, err
		}
		f := files[name]
		if f == nil || !f.Mode().IsRegular() {
			return domain.EPUBMetadata{}, domain.ErrInvalidEPUB
		}
	}
	return domain.EPUBMetadata{Title: titles[0], Authors: authors, Language: language,
		Identifiers: identifiers, PackageVersion: pkg.Version,
		HasEncryptionDescriptor: files["META-INF/encryption.xml"] != nil}, nil
}

func metadataValues(values []string, limit int) ([]string, error) {
	if len(values) > limit {
		return nil, domain.ErrInvalidEPUB
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if len(value) > 4096 {
			return nil, domain.ErrInvalidEPUB
		}
		if value != "" {
			result = append(result, value)
		}
	}
	return result, nil
}

func memberPath(base, value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\\\x00") {
		return "", domain.ErrInvalidEPUB
	}
	name := path.Join(base, u.Path)
	if !fs.ValidPath(name) || name == "." {
		return "", domain.ErrInvalidEPUB
	}
	return name, nil
}

func readMember(ctx context.Context, f *zip.File, limit int64) ([]byte, error) {
	if f == nil || !f.Mode().IsRegular() || f.Flags&1 != 0 || f.UncompressedSize64 > uint64(limit) || (f.Method != zip.Store && f.Method != zip.Deflate) {
		return nil, domain.ErrInvalidEPUB
	}
	r, err := f.Open()
	if err != nil {
		return nil, domain.ErrInvalidEPUB
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: r}, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.ErrInvalidEPUB
	}
	if int64(len(data)) > limit {
		return nil, domain.ErrInvalidEPUB
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func decodeXML(ctx context.Context, data []byte, target any) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return domain.ErrInvalidEPUB
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
			if depth > 64 || roots > 1 {
				return domain.ErrInvalidEPUB
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return domain.ErrInvalidEPUB
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(token)) != 0 {
				return domain.ErrInvalidEPUB
			}
		}
	}
	if depth != 0 || roots != 1 || xml.Unmarshal(data, target) != nil {
		return domain.ErrInvalidEPUB
	}
	return nil
}

// Preflight the complete classic central directory before archive/zip can
// allocate entries. ZIP64, split archives and appended data are not supported.
func boundedDirectory(data io.ReaderAt, size int64) (int, error) {
	n := min(size, int64(22+65535))
	tail := make([]byte, n)
	if _, err := data.ReadAt(tail, size-n); err != nil {
		return 0, domain.ErrInvalidEPUB
	}
	end := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if bytes.Equal(tail[i:i+4], []byte{'P', 'K', 5, 6}) && i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail) {
			end = i
			break
		}
	}
	if end < 0 {
		return 0, domain.ErrInvalidEPUB
	}
	e := tail[end:]
	// archive/zip also accepts an end record before trailing bytes. Reject
	// competing signatures in the comment so it selects our preflighted one.
	if bytes.Contains(e[22:], []byte{'P', 'K', 5, 6}) {
		return 0, domain.ErrInvalidEPUB
	}
	count := int(binary.LittleEndian.Uint16(e[10:]))
	length := int64(binary.LittleEndian.Uint32(e[12:]))
	offset := int64(binary.LittleEndian.Uint32(e[16:]))
	if binary.LittleEndian.Uint16(e[4:]) != 0 || binary.LittleEndian.Uint16(e[6:]) != 0 || int(binary.LittleEndian.Uint16(e[8:])) != count || count == 0 || count > maxZIPEntries || length > maxDirectoryBytes || offset+length != size-n+int64(end) {
		return 0, domain.ErrInvalidEPUB
	}
	central := make([]byte, length)
	if _, err := data.ReadAt(central, offset); err != nil {
		return 0, domain.ErrInvalidEPUB
	}
	// Do not let a ZIP64 locator hidden in a central-entry comment redirect
	// the standard reader to an unbounded alternate directory.
	if len(central) >= 20 && bytes.Equal(central[len(central)-20:len(central)-16], []byte{'P', 'K', 6, 7}) {
		return 0, domain.ErrInvalidEPUB
	}
	position := 0
	for i := 0; i < count; i++ {
		if position+46 > len(central) {
			return 0, domain.ErrInvalidEPUB
		}
		h := central[position : position+46]
		if !bytes.Equal(h[:4], []byte{'P', 'K', 1, 2}) || binary.LittleEndian.Uint16(h[34:]) != 0 || binary.LittleEndian.Uint32(h[20:]) == 0xffffffff || binary.LittleEndian.Uint32(h[24:]) == 0xffffffff || binary.LittleEndian.Uint32(h[42:]) == 0xffffffff {
			return 0, domain.ErrInvalidEPUB
		}
		position += 46 + int(binary.LittleEndian.Uint16(h[28:])) + int(binary.LittleEndian.Uint16(h[30:])) + int(binary.LittleEndian.Uint16(h[32:]))
		if position > len(central) {
			return 0, domain.ErrInvalidEPUB
		}
	}
	if position != len(central) {
		return 0, domain.ErrInvalidEPUB
	}
	return count, nil
}
