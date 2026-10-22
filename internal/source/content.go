package source

import (
	"context"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.SourceReader = (*Browser)(nil)

func (b *Browser) OpenContent(ctx context.Context, libraryID, relative string) (domain.SourceContent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case b.busy <- struct{}{}:
	default:
		return nil, domain.ErrDiscoveryBusy
	}
	return b.openContent(ctx, libraryID, relative, func() { <-b.busy })
}

func (b *Browser) openContent(ctx context.Context, libraryID, relative string, release func()) (domain.SourceContent, error) {
	if !utf8.ValidString(relative) || !fs.ValidPath(relative) || relative == "." || len(relative) > 4096 || strings.ContainsAny(relative, "\\\x00") {
		release()
		return nil, domain.ErrInvalidSourcePath
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	if err := b.ValidateLibrary(ctx, libraryID); err != nil {
		release()
		return nil, err
	}
	parent, name, owned, err := sourceParent(b.root, relative)
	if err != nil {
		release()
		return nil, err
	}
	f, err := openRegularSource(parent, name)
	if err != nil {
		if owned {
			_ = parent.Close()
		}
		release()
		return nil, domain.ErrSourceUnavailable
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if owned {
			_ = parent.Close()
		}
		release()
		return nil, domain.ErrSourceUnavailable
	}
	content := &sourceContent{file: f, initial: info, root: b.root, relative: relative, release: release}
	if owned {
		_ = parent.Close()
	}
	if err := content.VerifyUnchanged(); err != nil {
		_ = content.Close()
		return nil, err
	}
	return content, nil
}

// Inspect each component without accepting a declared symbolic link. os.Root
// also confines concurrent replacements to the configured source tree.
func sourceParent(root *os.Root, relative string) (*os.Root, string, bool, error) {
	parts := strings.Split(relative, "/")
	current, owned := root, false
	for _, part := range parts[:len(parts)-1] {
		info, err := current.Lstat(part)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			if owned {
				_ = current.Close()
			}
			return nil, "", false, domain.ErrSourceUnavailable
		}
		next, err := current.OpenRoot(part)
		if owned {
			_ = current.Close()
		}
		if err != nil {
			return nil, "", false, domain.ErrSourceUnavailable
		}
		current, owned = next, true
	}
	return current, parts[len(parts)-1], owned, nil
}

type sourceContent struct {
	file     *os.File
	initial  os.FileInfo
	root     *os.Root
	relative string
	release  func()
	once     sync.Once
	closeErr error
}

func (s *sourceContent) Read(p []byte) (int, error) {
	n, err := s.file.Read(p)
	if err != nil && err != io.EOF {
		return n, domain.ErrSourceUnavailable
	}
	return n, err
}
func (s *sourceContent) Info() os.FileInfo { return s.initial }
func (s *sourceContent) Close() error {
	s.once.Do(func() { s.closeErr = s.file.Close(); s.release() })
	return s.closeErr
}

func (s *sourceContent) VerifyUnchanged() error {
	opened, err := s.file.Stat()
	if err != nil {
		return domain.ErrSourceChanged
	}
	parent, name, owned, err := sourceParent(s.root, s.relative)
	if err != nil {
		return domain.ErrSourceChanged
	}
	if owned {
		defer parent.Close()
	}
	current, err := parent.Lstat(name)
	if err != nil {
		return domain.ErrSourceChanged
	}
	for _, info := range []os.FileInfo{opened, current} {
		if !info.Mode().IsRegular() || !os.SameFile(s.initial, info) || info.Size() != s.initial.Size() || !info.ModTime().Equal(s.initial.ModTime()) {
			return domain.ErrSourceChanged
		}
	}
	return nil
}
