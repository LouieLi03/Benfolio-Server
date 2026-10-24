package publication

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	MaxPDFBytes int64 = 128 << 20
	maxPDFPages       = 10_000
)

func ReadPDF(ctx context.Context, data io.ReadSeeker, size int64, fallbackTitle string) (metadata domain.BookMetadata, err error) {
	// Keep an unexpected parser panic from taking down the background worker.
	// Fatal runtime failures such as out-of-memory are not recoverable here.
	defer func() {
		if recover() != nil {
			metadata = domain.BookMetadata{}
			err = domain.ErrPDFParserFailure
		}
	}()
	if size <= 0 || size > MaxPDFBytes {
		return domain.BookMetadata{}, domain.ErrImportSize
	}
	if err := ctx.Err(); err != nil {
		return domain.BookMetadata{}, err
	}
	if _, err := data.Seek(0, io.SeekStart); err != nil {
		return domain.BookMetadata{}, err
	}
	// Per-operation stateless configuration avoids user configuration files,
	// font installation and process-wide mutable parser settings.
	conf := model.NewStatelessConfiguration()
	conf.Cmd = model.LISTINFO
	conf.ValidationMode = model.ValidationRelaxed
	conf.Offline = true
	conf.ValidateLinks = false
	conf.LocalImageRoot = ""
	conf.DecodeAllStreams = false
	conf.Optimize = false
	conf.OptimizeBeforeWriting = false
	conf.PreserveInfoDict = true
	conf.Limits = model.ResourceLimits{
		MaxInputBytes: MaxPDFBytes, MaxObjectBytes: 4 << 20,
		MaxStreamBytes: 32 << 20, MaxDecodeBytes: 32 << 20,
		MaxImagePixels: 16_000_000, MaxImageBytes: 64 << 20,
		MaxObjectCount: 100_000, MaxObjectStreamCount: 10_000,
		MaxObjectStreamFirst: 1 << 20, MaxXRefEntries: 100_000, MaxRecursionDepth: 64,
	}
	parsed, err := api.ReadAndValidate(ctx, data, conf)
	if err != nil {
		return domain.BookMetadata{}, pdfReadError(ctx, err)
	}
	if parsed == nil || parsed.XRefTable == nil {
		return domain.BookMetadata{}, domain.ErrInvalidPDF
	}
	// Even an encrypted file with an empty open-password remains encrypted.
	// Import never removes its protection or stores document passwords.
	if parsed.Encrypt != nil {
		return domain.BookMetadata{}, domain.ErrEncryptedPDF
	}
	if err := ctx.Err(); err != nil {
		return domain.BookMetadata{}, err
	}
	if parsed.PageCount < 1 || parsed.PageCount > maxPDFPages {
		return domain.BookMetadata{}, domain.ErrInvalidPDF
	}
	title, err := pdfText(parsed.Title)
	if err != nil {
		return domain.BookMetadata{}, err
	}
	if title == "" {
		title, err = pdfText(fallbackTitle)
		if err != nil || title == "" {
			return domain.BookMetadata{}, domain.ErrInvalidPDF
		}
	}
	author, err := pdfText(parsed.Author)
	if err != nil {
		return domain.BookMetadata{}, err
	}
	authors := []string{}
	if author != "" {
		authors = append(authors, author)
	}
	return domain.BookMetadata{Title: title, Authors: authors, Identifiers: []string{},
		PDF: &domain.PDFMetadata{PageCount: parsed.PageCount, Version: parsed.VersionString()}}, nil
}

func pdfReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrPermissionDenied) {
		return domain.ErrEncryptedPDF
	}
	return domain.ErrInvalidPDF
}

func pdfText(value string) (string, error) {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(value, "\ufffd"))
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 4096 {
		return "", domain.ErrInvalidPDF
	}
	return value, nil
}
