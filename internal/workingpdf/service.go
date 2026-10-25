// Package workingpdf materializes per-user annotation snapshots into PDF copies.
package workingpdf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"sync"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/localfile"
	"github.com/LouieLi03/Benfolio-Server/internal/publication"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

type Service struct {
	store             domain.WorkingPDFStore
	originals         domain.OriginalReader
	resources         domain.ResourceService
	state, working    *os.Root
	builds, downloads chan struct{}
}

var _ domain.WorkingPDFService = (*Service)(nil)

func New(store domain.WorkingPDFStore, originals domain.OriginalReader, resources domain.ResourceService, directory string) (*Service, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	for _, name := range []string{"staging", "working"} {
		info, err := root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = root.Close()
			return nil, domain.ErrResourceUnavailable
		}
	}
	working, err := root.OpenRoot("working")
	if err != nil {
		_ = root.Close()
		return nil, domain.ErrResourceUnavailable
	}
	return &Service{store: store, originals: originals, resources: resources, state: root, working: working, builds: make(chan struct{}, 1), downloads: make(chan struct{}, 2)}, nil
}

func (s *Service) Close() error { return errors.Join(s.working.Close(), s.state.Close()) }

func (s *Service) Status(ctx context.Context, userID, versionID string, revision int64) (domain.WorkingPDFStatus, error) {
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) || revision < 0 || revision > domain.MaxMutationRevision {
		return domain.WorkingPDFStatus{}, domain.ErrInvalidWorkingPDF
	}
	return s.store.WorkingStatus(ctx, userID, versionID, revision)
}

func (s *Service) Build(ctx context.Context, userID, versionID string, command domain.WorkingPDFMutation) (domain.WorkingPDFResult, error) {
	var result domain.WorkingPDFResult
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) || !syncvalue.ValidID(command.ID) || !syncvalue.ValidID(command.DeviceID) ||
		!syncvalue.ValidSHA256(command.SourceSHA256) || command.BaseRevision < 0 || command.BaseRevision >= domain.MaxMutationRevision || command.AnnotationSeq < 0 {
		return result, domain.ErrInvalidWorkingPDF
	}
	identity := struct {
		Operation string                    `json:"operation"`
		VersionID string                    `json:"versionID"`
		Command   domain.WorkingPDFMutation `json:"command"`
	}{"working-pdf.build.v1", versionID, command}
	data, err := json.Marshal(identity)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(data)
	requestHash := hex.EncodeToString(digest[:])
	if old, err := s.receipt(ctx, userID, versionID, command.ID, requestHash); old != nil || err != nil {
		if err != nil {
			return result, err
		}
		return *old, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	select {
	case s.builds <- struct{}{}:
	default:
		return result, domain.ErrWorkingPDFBusy
	}
	defer func() { <-s.builds }()
	if old, err := s.receipt(ctx, userID, versionID, command.ID, requestHash); old != nil || err != nil {
		if err != nil {
			return result, err
		}
		return *old, nil
	}
	snapshot, err := s.store.WorkingSnapshot(ctx, userID, versionID)
	if err != nil {
		return result, err
	}
	if snapshot.Status.Revision != command.BaseRevision || snapshot.Status.SourceSHA256 != command.SourceSHA256 || snapshot.Status.LatestAnnotationSeq != command.AnnotationSeq {
		return result, domain.ErrWorkingPDFConflict
	}
	if snapshot.Status.SourceByteSize > 32<<20 || snapshot.PageCount < 1 || snapshot.PageCount > 1000 {
		return result, domain.ErrWorkingPDFLimit
	}
	source, err := s.originals.OpenOriginal(ctx, versionID)
	if err != nil {
		return result, err
	}
	defer source.Content.Close()
	if source.Format != domain.PDF || source.Version.SHA256 != command.SourceSHA256 {
		return result, domain.ErrWorkingPDFConflict
	}
	id, err := domain.NewID()
	if err != nil {
		return result, err
	}
	temp := "staging/working-" + id + ".tmp"
	f, err := s.state.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return result, domain.ErrResourceUnavailable
	}
	defer func() { _ = f.Close(); _ = s.state.Remove(temp) }()
	writer := &boundedWriter{ctx: ctx, file: f, hash: sha256.New()}
	count, err := render(ctx, userID, source, snapshot, s.resources, writer)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, err
	}
	if writer.size < 1 {
		return result, domain.ErrWorkingPDFRender
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return result, domain.ErrResourceUnavailable
	}
	validated, err := publication.ReadPDF(ctx, f, writer.size, "工作PDF")
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, domain.ErrWorkingPDFRender
	}
	if validated.PDF == nil || validated.PDF.PageCount != snapshot.PageCount {
		return result, domain.ErrWorkingPDFRender
	}
	fileHash := hex.EncodeToString(writer.hash.Sum(nil))
	if err := f.Chmod(0o444); err != nil {
		return result, domain.ErrResourceUnavailable
	}
	if err := f.Sync(); err != nil {
		return result, domain.ErrResourceUnavailable
	}
	if err := f.Close(); err != nil {
		return result, domain.ErrResourceUnavailable
	}
	if err := s.publish(ctx, temp, fileHash, writer.size); err != nil {
		return result, err
	}
	return s.store.CommitWorkingPDF(ctx, userID, versionID, command, requestHash, fileHash, writer.size, count)
}

func (s *Service) receipt(ctx context.Context, userID, versionID, id, digest string) (*domain.WorkingPDFResult, error) {
	result, err := s.store.WorkingReceipt(ctx, userID, id, digest)
	if err != nil {
		return nil, err
	}
	if result != nil && result.WorkingPDF.DocumentVersionID != versionID {
		return nil, domain.ErrResourceConflict
	}
	return result, nil
}

func (s *Service) Open(ctx context.Context, userID, versionID string, revision int64) (domain.WorkingPDFFile, error) {
	var result domain.WorkingPDFFile
	if err := ctx.Err(); err != nil {
		return result, err
	}
	select {
	case s.downloads <- struct{}{}:
	default:
		return result, domain.ErrDownloadBusy
	}
	retained := false
	defer func() {
		if !retained {
			<-s.downloads
		}
	}()
	status, err := s.Status(ctx, userID, versionID, revision)
	if err != nil {
		return result, err
	}
	if !status.Available {
		return result, domain.ErrWorkingPDFNotFound
	}
	f, err := s.openFile(ctx, status.FileSHA256, status.ByteSize)
	if err != nil {
		return result, err
	}
	retained = true
	return domain.WorkingPDFFile{Status: status, Content: &content{file: f, reader: io.NewSectionReader(f, 0, status.ByteSize), ctx: ctx, release: func() { <-s.downloads }}}, nil
}

func (s *Service) publish(ctx context.Context, temp, digest string, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.working.Mkdir(digest[:2], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return domain.ErrResourceUnavailable
	}
	info, err := s.working.Lstat(digest[:2])
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return domain.ErrResourceConflict
	}
	if err := s.state.Link(temp, "working/"+digest[:2]+"/"+digest+".pdf"); err != nil && !errors.Is(err, os.ErrExist) {
		return domain.ErrResourceUnavailable
	}
	f, err := s.openFile(ctx, digest, size)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return domain.ErrResourceUnavailable
	}
	for _, name := range []string{"working/" + digest[:2], "working"} {
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
	if !syncvalue.ValidSHA256(digest) || size < 1 || size > domain.MaxWorkingPDFBytes {
		return nil, domain.ErrResourceConflict
	}
	info, err := s.working.Lstat(digest[:2])
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, domain.ErrResourceConflict
	}
	shard, err := s.working.OpenRoot(digest[:2])
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	defer shard.Close()
	name := digest + ".pdf"
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

type boundedWriter struct {
	ctx  context.Context
	file *os.File
	hash hash.Hash
	size int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > domain.MaxWorkingPDFBytes-w.size {
		return 0, domain.ErrWorkingPDFLimit
	}
	n, err := w.file.Write(p)
	if n > 0 {
		_, _ = w.hash.Write(p[:n])
		w.size += int64(n)
	}
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
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

type content struct {
	ctx     context.Context
	file    *os.File
	reader  *io.SectionReader
	release func()
	once    sync.Once
	err     error
}

func (c *content) Read(p []byte) (int, error) {
	return (&contextReader{ctx: c.ctx, reader: c.reader}).Read(p)
}
func (c *content) Seek(offset int64, whence int) (int64, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.reader.Seek(offset, whence)
}
func (c *content) Close() error {
	c.once.Do(func() { c.err = c.file.Close(); c.release() })
	return c.err
}
