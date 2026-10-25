// Package resource archives immutable portable ink resources owned by an account.
package resource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/localfile"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

type Service struct {
	store     domain.ResourceStore
	state     *os.Root
	resources *os.Root
	slots     chan struct{}
}

var _ domain.ResourceService = (*Service)(nil)

func New(store domain.ResourceStore, directory string) (*Service, error) {
	state, err := os.OpenRoot(directory)
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	for _, name := range []string{"staging", "resources"} {
		info, err := state.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = state.Close()
			return nil, domain.ErrResourceUnavailable
		}
	}
	resources, err := state.OpenRoot("resources")
	if err != nil {
		_ = state.Close()
		return nil, domain.ErrResourceUnavailable
	}
	return &Service{store: store, state: state, resources: resources, slots: make(chan struct{}, 2)}, nil
}

func (s *Service) Close() error { return errors.Join(s.resources.Close(), s.state.Close()) }

func (s *Service) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	default:
		return domain.ErrResourceBusy
	}
}

func (s *Service) Put(ctx context.Context, userID, digest string, body io.Reader) (domain.ResourceResult, error) {
	var result domain.ResourceResult
	if !syncvalue.ValidID(userID) || !syncvalue.ValidSHA256(digest) {
		return result, domain.ErrInvalidResource
	}
	if err := s.acquire(ctx); err != nil {
		return result, err
	}
	defer func() { <-s.slots }()
	data, err := io.ReadAll(io.LimitReader(&checkedReader{ctx: ctx, reader: body}, domain.MaxInkBytes+1))
	if err != nil {
		return result, err
	}
	if len(data) == 0 || int64(len(data)) > domain.MaxInkBytes {
		return result, domain.ErrInvalidResource
	}
	if hash(data) != digest {
		return result, domain.ErrResourceDigest
	}
	ink, err := DecodeInk(ctx, data)
	if err != nil {
		return result, err
	}
	version, err := s.store.Version(ctx, ink.DocumentVersionID)
	if err != nil {
		return result, err
	}
	if version.Format != domain.PDF || version.SHA256 != ink.SourceSHA256 || version.Metadata.PDF == nil ||
		*ink.PageIndex >= int64(version.Metadata.PDF.PageCount) {
		return result, domain.ErrResourceBinding
	}
	key, err := s.publish(ctx, digest, data)
	if err != nil {
		return result, err
	}
	record := domain.ResourceRecord{ObjectKey: key, Info: domain.ResourceInfo{
		SHA256: digest, ByteSize: int64(len(data)), MediaType: domain.InkMediaType,
		DocumentVersionID: ink.DocumentVersionID, SourceSHA256: ink.SourceSHA256, PageIndex: *ink.PageIndex,
		SchemaVersion: ink.SchemaVersion, CoordinateSpace: ink.CoordinateSpace,
	}}
	return s.store.RegisterResource(ctx, userID, record)
}

func (s *Service) Open(ctx context.Context, userID, digest string) (domain.ResourceFile, error) {
	var result domain.ResourceFile
	if !syncvalue.ValidID(userID) || !syncvalue.ValidSHA256(digest) {
		return result, domain.ErrInvalidResource
	}
	if err := s.acquire(ctx); err != nil {
		return result, err
	}
	retained := false
	defer func() {
		if !retained {
			<-s.slots
		}
	}()
	record, err := s.store.ReadResource(ctx, userID, digest)
	if err != nil {
		return result, err
	}
	r := record.Info
	if r.SHA256 != digest || r.ByteSize < 1 || r.ByteSize > domain.MaxInkBytes || r.MediaType != domain.InkMediaType ||
		r.SchemaVersion != 1 || r.CoordinateSpace != domain.InkCoordinateSpace || !syncvalue.ValidID(r.DocumentVersionID) ||
		!syncvalue.ValidSHA256(r.SourceSHA256) || r.PageIndex < 0 || record.ObjectKey != objectKey(digest) {
		return result, domain.ErrResourceConflict
	}
	data, err := s.read(ctx, digest, r.ByteSize)
	if err != nil {
		return result, err
	}
	retained = true
	return domain.ResourceFile{Resource: r, Content: &content{reader: bytes.NewReader(data), ctx: ctx, release: func() { <-s.slots }}}, nil
}

func objectKey(digest string) string { return digest[:2] + "/" + digest + ".ink.json" }
func hash(data []byte) string        { digest := sha256.Sum256(data); return hex.EncodeToString(digest[:]) }

func (s *Service) read(ctx context.Context, digest string, size int64) ([]byte, error) {
	info, err := s.resources.Lstat(digest[:2])
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, domain.ErrResourceConflict
	}
	shard, err := s.resources.OpenRoot(digest[:2])
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	defer shard.Close()
	name := digest + ".ink.json"
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
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if !os.SameFile(named, opened) || opened.Size() != size {
		return nil, domain.ErrResourceConflict
	}
	data, err := io.ReadAll(io.LimitReader(&checkedReader{ctx: ctx, reader: f}, size+1))
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
	if int64(len(data)) != size || hash(data) != digest || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return nil, domain.ErrResourceConflict
	}
	return data, ctx.Err()
}

func (s *Service) publish(ctx context.Context, digest string, data []byte) (string, error) {
	id, err := domain.NewID()
	if err != nil {
		return "", err
	}
	temp := "staging/ink-" + id + ".tmp"
	f, err := s.state.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", domain.ErrResourceUnavailable
	}
	defer func() { _ = f.Close(); _ = s.state.Remove(temp) }()
	if n, err := f.Write(data); err != nil || n != len(data) {
		return "", domain.ErrResourceUnavailable
	}
	if err := f.Chmod(0o444); err != nil {
		return "", domain.ErrResourceUnavailable
	}
	if err := f.Sync(); err != nil {
		return "", domain.ErrResourceUnavailable
	}
	if err := f.Close(); err != nil {
		return "", domain.ErrResourceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.resources.Mkdir(digest[:2], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", domain.ErrResourceUnavailable
	}
	info, err := s.resources.Lstat(digest[:2])
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", domain.ErrResourceConflict
	}
	key := objectKey(digest)
	if err := s.state.Link(temp, "resources/"+key); err != nil && !errors.Is(err, os.ErrExist) {
		return "", domain.ErrResourceUnavailable
	}
	if _, err := s.read(ctx, digest, int64(len(data))); err != nil {
		return "", err
	}
	for _, name := range []string{"resources/" + digest[:2], "resources"} {
		dir, err := s.state.Open(name)
		if err != nil {
			return "", domain.ErrResourceUnavailable
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil || closeErr != nil {
			return "", domain.ErrResourceUnavailable
		}
	}
	return key, ctx.Err()
}

type checkedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *checkedReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type content struct {
	reader  *bytes.Reader
	ctx     context.Context
	release func()
	once    sync.Once
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
func (c *content) Close() error { c.once.Do(c.release); return nil }
