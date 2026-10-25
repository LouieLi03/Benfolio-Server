package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/localfile"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

func (s *Service) publish(ctx context.Context, digest string, data []byte) error {
	if len(data) == 0 || int64(len(data)) > domain.MaxMarkdownBytes {
		return domain.ErrExportLimit
	}
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	temp := "staging/export-" + id + ".tmp"
	f, err := s.state.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return domain.ErrResourceUnavailable
	}
	defer func() { _ = f.Close(); _ = s.state.Remove(temp) }()
	if n, err := f.Write(data); err != nil || n != len(data) {
		return domain.ErrResourceUnavailable
	}
	if err := f.Chmod(0o444); err != nil {
		return domain.ErrResourceUnavailable
	}
	if err := f.Sync(); err != nil {
		return domain.ErrResourceUnavailable
	}
	if err := f.Close(); err != nil {
		return domain.ErrResourceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Mkdir(digest[:2], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return domain.ErrResourceUnavailable
	}
	info, err := s.root.Lstat(digest[:2])
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.ErrResourceConflict
	}
	if err := s.state.Link(temp, "exports/"+digest[:2]+"/"+digest+".md"); err != nil && !errors.Is(err, os.ErrExist) {
		return domain.ErrResourceUnavailable
	}
	check, err := s.openFile(ctx, digest, int64(len(data)))
	if err != nil {
		return err
	}
	if err := check.Close(); err != nil {
		return domain.ErrResourceUnavailable
	}
	for _, name := range []string{"exports/" + digest[:2], "exports"} {
		dir, err := s.state.Open(name)
		if err != nil {
			return domain.ErrResourceUnavailable
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil || closeErr != nil {
			return domain.ErrResourceUnavailable
		}
	}
	return ctx.Err()
}

func (s *Service) openFile(ctx context.Context, digest string, size int64) (*os.File, error) {
	if !syncvalue.ValidSHA256(digest) || size < 1 || size > domain.MaxMarkdownBytes {
		return nil, domain.ErrResourceConflict
	}
	info, err := s.root.Lstat(digest[:2])
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, domain.ErrResourceConflict
	}
	shard, err := s.root.OpenRoot(digest[:2])
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	defer shard.Close()
	name := digest + ".md"
	named, err := shard.Lstat(name)
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if !named.Mode().IsRegular() {
		return nil, domain.ErrResourceConflict
	}
	f, err := localfile.OpenRegular(shard, name)
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
		}
	}()
	before, err := f.Stat()
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if !os.SameFile(named, before) || before.Size() != size {
		return nil, domain.ErrResourceConflict
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, io.LimitReader(&contextReader{ctx: ctx, reader: f}, size+1), make([]byte, 64<<10))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.ErrResourceUnavailable
	}
	after, err := f.Stat()
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != digest || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, domain.ErrResourceConflict
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ok = true
	return f, nil
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

type markdownFile struct {
	file   *os.File
	reader *io.SectionReader
	ctx    context.Context
}

func (f *markdownFile) Read(p []byte) (int, error) {
	return (&contextReader{ctx: f.ctx, reader: f.reader}).Read(p)
}
func (f *markdownFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.reader.Seek(offset, whence)
}
func (f *markdownFile) Close() error { return f.file.Close() }
