package source

import (
	"context"
	"errors"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (b *Browser) ScanForImport(ctx context.Context, libraryID string, visit func(domain.SourceFile, domain.SourceReader) error) (domain.SourceDiscovery, error) {
	if visit == nil {
		return domain.SourceDiscovery{}, errors.New("扫描需要导入处理函数")
	}
	select {
	case b.busy <- struct{}{}:
		defer func() { <-b.busy }()
	default:
		return domain.SourceDiscovery{}, domain.ErrDiscoveryBusy
	}
	if err := b.ValidateLibrary(ctx, libraryID); err != nil {
		return domain.SourceDiscovery{}, err
	}
	return b.discover(ctx, libraryID, 0, 1_000_000, 64, func(file domain.SourceFile) error {
		// A callback may open the current candidate only, using this scan's
		// existing access slot rather than recursively acquiring it.
		fileCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		return visit(file, &scanReader{browser: b, ctx: fileCtx, libraryID: libraryID, relative: file.RelativePath})
	})
}

type scanReader struct {
	browser   *Browser
	ctx       context.Context
	libraryID string
	relative  string
}

func (s *scanReader) OpenContent(ctx context.Context, libraryID, relative string) (domain.SourceContent, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if libraryID != s.libraryID || relative != s.relative {
		return nil, domain.ErrInvalidSourcePath
	}
	return s.browser.openContent(ctx, libraryID, relative, func() {})
}
