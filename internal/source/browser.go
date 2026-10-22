package source

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

const (
	entryLimit = 10_000
	depthLimit = 32
	batchSize  = 128
)

type Browser struct {
	catalog   domain.Catalog
	libraryID string
	root      *os.Root
	busy      chan struct{}
}

var _ domain.SourceBrowser = (*Browser)(nil)
var _ domain.SourceScanner = (*Browser)(nil)

func NewBrowser(catalog domain.Catalog, libraryID, sourcePath string) (*Browser, error) {
	b := &Browser{catalog: catalog, libraryID: libraryID, busy: make(chan struct{}, 1)}
	if libraryID == "" && sourcePath == "" {
		return b, nil
	}
	if libraryID == "" || sourcePath == "" {
		return nil, domain.ErrSourceNotConfigured
	}
	root, err := os.OpenRoot(sourcePath)
	if err != nil {
		return nil, domain.ErrSourceUnavailable
	}
	b.root = root
	return b, nil
}

func (b *Browser) Close() error {
	if b.root == nil {
		return nil
	}
	return b.root.Close()
}

func (b *Browser) Files(ctx context.Context, libraryID string, limit int) (domain.SourceDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.SourceDiscovery{}, err
	}
	if limit < 1 || limit > 200 {
		return domain.SourceDiscovery{}, errors.New("文件发现条数必须在 1–200 之间")
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
	return b.discover(ctx, libraryID, limit, entryLimit, depthLimit, nil)
}

func (b *Browser) ValidateLibrary(ctx context.Context, libraryID string) error {
	libraries, err := b.catalog.Libraries(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, library := range libraries {
		if library.ID == libraryID {
			found = true
			break
		}
	}
	if !found {
		return domain.ErrLibraryNotFound
	}
	if libraryID != b.libraryID || b.root == nil {
		return domain.ErrSourceNotConfigured
	}
	return nil
}

// Scan streams candidates without the preview's returned-file limit. Bounds and
// skipped entries are still reported; incomplete discovery is never full success.
func (b *Browser) Scan(ctx context.Context, libraryID string, visit func(domain.SourceFile) error) (domain.SourceDiscovery, error) {
	if visit == nil {
		return domain.SourceDiscovery{}, errors.New("扫描需要候选文件处理函数")
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
	return b.discover(ctx, libraryID, 0, 1_000_000, 64, visit)
}

func (b *Browser) discover(ctx context.Context, libraryID string, limit, entries, depth int, visit func(domain.SourceFile) error) (domain.SourceDiscovery, error) {
	walk := discoveryWalk{result: domain.SourceDiscovery{
		LibraryID: libraryID, Items: make([]domain.SourceFile, 0), Complete: true,
		IncompleteReasons: make([]string, 0), Limit: limit, EntryLimit: entries, DepthLimit: depth,
	}, visit: visit}
	_, err := walk.directory(ctx, b.root, "", 0)
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	if err != nil {
		walk.incomplete("discovery_error")
		return walk.result, err
	}
	sort.Slice(walk.result.Items, func(i, j int) bool {
		return walk.result.Items[i].RelativePath < walk.result.Items[j].RelativePath
	})
	return walk.result, nil
}

type discoveryWalk struct {
	result domain.SourceDiscovery
	visit  func(domain.SourceFile) error
}

func (w *discoveryWalk) incomplete(reason string) {
	w.result.Complete = false
	for _, existing := range w.result.IncompleteReasons {
		if existing == reason {
			return
		}
	}
	w.result.IncompleteReasons = append(w.result.IncompleteReasons, reason)
}

// Directory handles and metadata lookups stay within os.Root. Enumeration reads
// metadata; an import visitor may read contents. This is not a source snapshot.
func (w *discoveryWalk) directory(ctx context.Context, root *os.Root, relative string, depth int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	directory, err := root.Open(".")
	if err != nil {
		return false, domain.ErrSourceUnavailable
	}
	defer directory.Close()
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		entries, readErr := directory.ReadDir(batchSize)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if w.result.VisitedEntries >= w.result.EntryLimit {
				w.incomplete("entry_limit")
				return true, nil
			}
			w.result.VisitedEntries++
			if !utf8.ValidString(entry.Name()) {
				w.result.SkippedInvalidNames++
				w.incomplete("invalid_name")
				continue
			}
			info, err := root.Lstat(entry.Name())
			if errors.Is(err, os.ErrNotExist) {
				w.result.SkippedChangedEntries++
				w.incomplete("source_changed")
				continue
			}
			if err != nil {
				return false, domain.ErrSourceUnavailable
			}
			if info.Mode()&os.ModeSymlink != 0 {
				w.result.SkippedSymlinks++
				continue
			}
			name := filepath.Join(relative, entry.Name())
			if info.IsDir() {
				if depth >= w.result.DepthLimit {
					w.result.SkippedDirectories++
					w.incomplete("depth_limit")
					continue
				}
				child, err := root.OpenRoot(entry.Name())
				if errors.Is(err, os.ErrNotExist) {
					w.result.SkippedChangedEntries++
					w.incomplete("source_changed")
					continue
				}
				if err != nil {
					return false, domain.ErrSourceUnavailable
				}
				stop, err := w.directory(ctx, child, name, depth+1)
				_ = child.Close()
				if err != nil || stop {
					return stop, err
				}
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			var format domain.Format
			switch strings.ToLower(filepath.Ext(entry.Name())) {
			case ".epub":
				format = domain.EPUB
			case ".pdf":
				format = domain.PDF
			default:
				continue
			}
			if w.result.Limit > 0 && len(w.result.Items) >= w.result.Limit {
				w.incomplete("file_limit")
				return true, nil
			}
			candidate := domain.SourceFile{
				RelativePath: filepath.ToSlash(name), Format: format,
				ByteSize: info.Size(), ModifiedAt: info.ModTime().UTC(),
			}
			w.result.CandidateFiles++
			if w.visit != nil {
				if err := w.visit(candidate); err != nil {
					return false, err
				}
			} else {
				w.result.Items = append(w.result.Items, candidate)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return false, nil
		}
		if readErr != nil {
			return false, domain.ErrSourceUnavailable
		}
	}
}
