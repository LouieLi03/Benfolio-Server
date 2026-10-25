// Package export publishes immutable PDF and readable note export records.
package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

type Service struct {
	store              domain.ExportStore
	working            domain.WorkingPDFService
	state, root        *os.Root
	creates, downloads chan struct{}
}

var _ domain.ExportService = (*Service)(nil)

func New(store domain.ExportStore, working domain.WorkingPDFService, directory string) (*Service, error) {
	state, err := os.OpenRoot(directory)
	if err != nil {
		return nil, domain.ErrResourceUnavailable
	}
	for _, name := range []string{"staging", "exports"} {
		info, err := state.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = state.Close()
			return nil, domain.ErrResourceUnavailable
		}
	}
	root, err := state.OpenRoot("exports")
	if err != nil {
		_ = state.Close()
		return nil, domain.ErrResourceUnavailable
	}
	return &Service{store: store, working: working, state: state, root: root, creates: make(chan struct{}, 1), downloads: make(chan struct{}, 2)}, nil
}

func (s *Service) Close() error { return errors.Join(s.root.Close(), s.state.Close()) }

func (s *Service) Context(ctx context.Context, userID, versionID string) (domain.ExportContext, error) {
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) {
		return domain.ExportContext{}, domain.ErrInvalidExport
	}
	return s.store.ExportContext(ctx, userID, versionID)
}

func (s *Service) receipt(ctx context.Context, userID, versionID, id, digest string) (*domain.ExportResult, error) {
	result, err := s.store.ExportReceipt(ctx, userID, id, digest)
	if err != nil {
		return nil, err
	}
	if result != nil && result.Export.DocumentVersionID != versionID {
		return nil, domain.ErrResourceConflict
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, userID, versionID string, command domain.ExportMutation) (domain.ExportResult, error) {
	var result domain.ExportResult
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) || !syncvalue.ValidID(command.ID) || !syncvalue.ValidID(command.DeviceID) ||
		!syncvalue.ValidSHA256(command.SourceSHA256) || command.AnnotationSeq < 0 || command.WorkingRevision < 0 || command.WorkingRevision > domain.MaxMutationRevision ||
		(command.Format != "pdf" && command.Format != "markdown") || command.Format == "pdf" && command.WorkingRevision == 0 || command.Format == "markdown" && command.WorkingRevision != 0 {
		return result, domain.ErrInvalidExport
	}
	identity := struct {
		Operation string                `json:"operation"`
		VersionID string                `json:"versionID"`
		Command   domain.ExportMutation `json:"command"`
	}{"export.create.v1", versionID, command}
	raw, err := json.Marshal(identity)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	requestHash := hex.EncodeToString(sum[:])
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
	case s.creates <- struct{}{}:
	default:
		return result, domain.ErrExportBusy
	}
	defer func() { <-s.creates }()
	if old, err := s.receipt(ctx, userID, versionID, command.ID, requestHash); old != nil || err != nil {
		if err != nil {
			return result, err
		}
		return *old, nil
	}
	info := domain.ExportInfo{ID: command.ID, DocumentVersionID: versionID, SourceSHA256: command.SourceSHA256, Format: command.Format,
		AnnotationSeq: command.AnnotationSeq, WorkingRevision: command.WorkingRevision, DeviceID: command.DeviceID, CreatedAt: time.Now().UTC()}
	if command.Format == "pdf" {
		file, err := s.working.Open(ctx, userID, versionID, command.WorkingRevision)
		if err != nil {
			return result, err
		}
		if file.Status.SourceSHA256 != command.SourceSHA256 || file.Status.MaterializedChangeSeq != command.AnnotationSeq {
			_ = file.Content.Close()
			return result, domain.ErrExportConflict
		}
		info.FileSHA256, info.ByteSize, info.BookFormat, info.AnnotationCount = file.Status.FileSHA256, file.Status.ByteSize, domain.PDF, file.Status.AnnotationCount
		if err := file.Content.Close(); err != nil {
			return result, err
		}
	} else {
		snapshot, err := s.store.ExportSnapshot(ctx, userID, versionID)
		if err != nil {
			return result, err
		}
		if snapshot.SourceSHA256 != command.SourceSHA256 || snapshot.AnnotationSeq != command.AnnotationSeq {
			return result, domain.ErrExportConflict
		}
		data, err := markdown(ctx, snapshot, info.CreatedAt)
		if err != nil {
			return result, err
		}
		sum := sha256.Sum256(data)
		info.FileSHA256 = hex.EncodeToString(sum[:])
		info.ByteSize = int64(len(data))
		info.BookFormat = snapshot.BookFormat
		info.AnnotationCount = len(snapshot.Annotations)
		if err := s.publish(ctx, info.FileSHA256, data); err != nil {
			return result, err
		}
	}
	return s.store.CommitExport(ctx, userID, versionID, command, requestHash, info)
}

func (s *Service) Get(ctx context.Context, userID, id string) (domain.ExportInfo, error) {
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(id) {
		return domain.ExportInfo{}, domain.ErrInvalidExport
	}
	return s.store.ReadExport(ctx, userID, id)
}

func (s *Service) Open(ctx context.Context, userID, id string) (domain.ExportFile, error) {
	var result domain.ExportFile
	if err := ctx.Err(); err != nil {
		return result, err
	}
	select {
	case s.downloads <- struct{}{}:
	default:
		return result, domain.ErrExportBusy
	}
	retained := false
	defer func() {
		if !retained {
			<-s.downloads
		}
	}()
	info, err := s.Get(ctx, userID, id)
	if err != nil {
		return result, err
	}
	var reader domain.OriginalContent
	if info.Format == "pdf" {
		file, err := s.working.Open(ctx, userID, info.DocumentVersionID, info.WorkingRevision)
		if err != nil {
			return result, err
		}
		if file.Status.FileSHA256 != info.FileSHA256 || file.Status.ByteSize != info.ByteSize || file.Status.MaterializedChangeSeq != info.AnnotationSeq || file.Status.SourceSHA256 != info.SourceSHA256 {
			_ = file.Content.Close()
			return result, domain.ErrResourceConflict
		}
		reader = file.Content
	} else {
		f, err := s.openFile(ctx, info.FileSHA256, info.ByteSize)
		if err != nil {
			return result, err
		}
		reader = &markdownFile{file: f, reader: io.NewSectionReader(f, 0, info.ByteSize), ctx: ctx}
	}
	retained = true
	return domain.ExportFile{Export: info, Content: &exportContent{inner: reader, release: func() { <-s.downloads }}}, nil
}

type exportContent struct {
	inner   domain.OriginalContent
	release func()
	once    sync.Once
	err     error
}

func (c *exportContent) Read(p []byte) (int, error) { return c.inner.Read(p) }
func (c *exportContent) Seek(offset int64, whence int) (int64, error) {
	return c.inner.Seek(offset, whence)
}
func (c *exportContent) Close() error {
	c.once.Do(func() { c.err = c.inner.Close(); c.release() })
	return c.err
}
