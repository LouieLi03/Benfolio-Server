package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"strings"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/publication"
)

type Service struct {
	store     domain.ImportStore
	sources   domain.SourceReader
	staging   string
	originals string
}

var _ domain.BookImporter = (*Service)(nil)
var _ domain.ScanBookImporter = (*Service)(nil)

func New(store domain.ImportStore, sources domain.SourceReader, staging, originals string) *Service {
	return &Service{store: store, sources: sources, staging: staging, originals: originals}
}

func (s *Service) ImportBook(ctx context.Context, libraryID, relative string) (domain.ImportResult, error) {
	return s.ImportBookFrom(ctx, s.sources, libraryID, relative)
}

// The scan supplies a scoped reader while already holding source access.
func (s *Service) ImportBookFrom(ctx context.Context, reader domain.SourceReader, libraryID, relative string) (domain.ImportResult, error) {
	format := domain.Format(strings.TrimPrefix(strings.ToLower(path.Ext(relative)), "."))
	if format != domain.EPUB && format != domain.PDF {
		return domain.ImportResult{}, domain.ErrUnsupportedBook
	}
	source, err := reader.OpenContent(ctx, libraryID, relative)
	if err != nil {
		return domain.ImportResult{}, err
	}
	defer source.Close()
	size := source.Info().Size()
	limit := publication.MaxEPUBBytes
	if format == domain.PDF {
		limit = publication.MaxPDFBytes
	}
	if size <= 0 || size > limit {
		return domain.ImportResult{}, domain.ErrImportSize
	}
	temp, err := os.CreateTemp(s.staging, "book-import-*")
	if err != nil {
		return domain.ImportResult{}, err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	hash := sha256.New()
	n, err := io.CopyBuffer(io.MultiWriter(temp, hash), io.LimitReader(&contextReader{ctx: ctx, reader: source}, size+1), make([]byte, 64<<10))
	if err != nil {
		return domain.ImportResult{}, err
	}
	if n != size {
		return domain.ImportResult{}, domain.ErrSourceChanged
	}
	if err := source.VerifyUnchanged(); err != nil {
		return domain.ImportResult{}, err
	}
	var metadata domain.BookMetadata
	if format == domain.EPUB {
		metadata, err = publication.ReadEPUB(ctx, temp, size)
	} else {
		metadata, err = publication.ReadPDF(ctx, temp, size, strings.TrimSuffix(path.Base(relative), path.Ext(relative)))
	}
	if err != nil {
		return domain.ImportResult{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if err := temp.Chmod(0o400); err != nil {
		return domain.ImportResult{}, err
	}
	if err := temp.Sync(); err != nil {
		return domain.ImportResult{}, err
	}
	if err := temp.Close(); err != nil {
		return domain.ImportResult{}, err
	}
	key, err := s.publishOriginal(ctx, temp.Name(), digest, size, format)
	if err != nil {
		return domain.ImportResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.ImportResult{}, err
	}
	if err := source.VerifyUnchanged(); err != nil {
		return domain.ImportResult{}, err
	}
	return s.store.SaveBook(ctx, domain.ImportRecord{LibraryID: libraryID, RelativePath: relative,
		SHA256: digest, ByteSize: size, ObjectKey: key, Format: format, Metadata: metadata})
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
