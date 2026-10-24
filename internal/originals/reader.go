// Package originals serves registered immutable archives, independently of the
// mutable library source mount. Unregistered state files cannot be downloaded.
package originals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/localfile"
)

const maxOriginalBytes int64 = 512 << 20

type Reader struct {
	catalog domain.OriginalCatalog
	root    *os.Root
	slots   chan struct{}
}

var _ domain.OriginalReader = (*Reader)(nil)

func New(catalog domain.OriginalCatalog, directory string) (*Reader, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, domain.ErrOriginalUnavailable
	}
	return &Reader{catalog: catalog, root: root, slots: make(chan struct{}, 2)}, nil
}

func (r *Reader) Close() error { return r.root.Close() }

func (r *Reader) OpenOriginal(ctx context.Context, versionID string) (domain.OriginalFile, error) {
	if err := ctx.Err(); err != nil {
		return domain.OriginalFile{}, err
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return domain.OriginalFile{}, domain.ErrDownloadBusy
	}
	retained := false
	defer func() {
		if !retained {
			<-r.slots
		}
	}()
	lookupCtx, cancelLookup := context.WithTimeout(ctx, 5*time.Second)
	record, err := r.catalog.Original(lookupCtx, versionID)
	cancelLookup()
	if err != nil {
		return domain.OriginalFile{}, err
	}
	digest := record.Version.SHA256
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) || record.Version.ByteSize <= 0 || record.Version.ByteSize > maxOriginalBytes || (record.Format != domain.EPUB && record.Format != domain.PDF) {
		return domain.OriginalFile{}, domain.ErrArchiveConflict
	}
	filename := digest + "." + string(record.Format)
	if record.ObjectKey != digest[:2]+"/"+filename {
		return domain.OriginalFile{}, domain.ErrArchiveConflict
	}
	info, err := r.root.Lstat(digest[:2])
	if err != nil {
		return domain.OriginalFile{}, domain.ErrOriginalUnavailable
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.OriginalFile{}, domain.ErrArchiveConflict
	}
	shard, err := r.root.OpenRoot(digest[:2])
	if err != nil {
		return domain.OriginalFile{}, domain.ErrOriginalUnavailable
	}
	defer shard.Close()
	named, err := shard.Lstat(filename)
	if err != nil {
		return domain.OriginalFile{}, domain.ErrOriginalUnavailable
	}
	if !named.Mode().IsRegular() {
		return domain.OriginalFile{}, domain.ErrArchiveConflict
	}
	f, err := localfile.OpenRegular(shard, filename)
	if err != nil {
		return domain.OriginalFile{}, domain.ErrOriginalUnavailable
	}
	defer func() {
		if !retained {
			_ = f.Close()
		}
	}()
	opened, err := f.Stat()
	if err != nil {
		return domain.OriginalFile{}, domain.ErrOriginalUnavailable
	}
	if !os.SameFile(named, opened) || opened.Size() != record.Version.ByteSize {
		return domain.OriginalFile{}, domain.ErrArchiveConflict
	}
	verifyCtx, cancelVerify := context.WithTimeout(ctx, 20*time.Second)
	err = verify(verifyCtx, f, digest, record.Version.ByteSize)
	cancelVerify()
	if err != nil {
		return domain.OriginalFile{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return domain.OriginalFile{}, domain.ErrOriginalUnavailable
	}
	if after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return domain.OriginalFile{}, domain.ErrArchiveConflict
	}
	if err := ctx.Err(); err != nil {
		return domain.OriginalFile{}, err
	}
	content := &content{file: f, reader: io.NewSectionReader(f, 0, record.Version.ByteSize), ctx: ctx, release: func() { <-r.slots }}
	retained = true
	return domain.OriginalFile{Content: content, Version: record.Version, Format: record.Format}, nil
}

func verify(ctx context.Context, f *os.File, expected string, size int64) error {
	hash := sha256.New()
	reader := &checkedReader{ctx: ctx, reader: io.LimitReader(f, size+1)}
	n, err := io.CopyBuffer(hash, reader, make([]byte, 64<<10))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(hash.Sum(nil)) != expected {
		return domain.ErrArchiveConflict
	}
	return nil
}

type checkedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *checkedReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF {
		return n, domain.ErrOriginalUnavailable
	}
	return n, err
}

type content struct {
	file     *os.File
	reader   *io.SectionReader
	ctx      context.Context
	release  func()
	once     sync.Once
	closeErr error
}

func (c *content) Read(p []byte) (int, error) {
	return (&checkedReader{ctx: c.ctx, reader: c.reader}).Read(p)
}
func (c *content) Seek(offset int64, whence int) (int64, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.reader.Seek(offset, whence)
}
func (c *content) Close() error {
	c.once.Do(func() { c.closeErr = c.file.Close(); c.release() })
	return c.closeErr
}
