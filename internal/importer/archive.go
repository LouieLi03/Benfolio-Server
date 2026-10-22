package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

// A complete, synced snapshot becomes visible before the database references
// it. Hard linking is atomic and never replaces an existing object. A failed
// database commit may leave an unreferenced original; it is never auto-deleted.
func (s *Service) publishOriginal(ctx context.Context, tempPath, digest string, size int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := path.Join(digest[:2], digest+".epub")
	directory := filepath.Join(s.originals, digest[:2])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(s.originals, filepath.FromSlash(key))
	if err := os.Link(tempPath, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if err := verifyOriginal(ctx, target, digest, size); err != nil {
			return "", err
		}
	}
	// Sync both the object directory and its parent (a new shard may have been
	// created). State storage must support local hard links and directory sync.
	for _, name := range []string{directory, s.originals} {
		dir, err := os.Open(name)
		if err != nil {
			return "", err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return key, nil
}

func verifyOriginal(ctx context.Context, name, expected string, size int64) error {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return domain.ErrArchiveConflict
	}
	f, err := os.Open(name)
	if err != nil {
		return domain.ErrArchiveConflict
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return domain.ErrArchiveConflict
	}
	hash := sha256.New()
	n, err := io.CopyBuffer(hash, io.LimitReader(&contextReader{ctx: ctx, reader: f}, size+1), make([]byte, 64<<10))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.ErrArchiveConflict
	}
	if n != size || hex.EncodeToString(hash.Sum(nil)) != expected {
		return domain.ErrArchiveConflict
	}
	return nil
}
