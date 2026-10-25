// Package progress validates version-bound reading positions before persistence.
package progress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
	"github.com/LouieLi03/Benfolio-Server/internal/position"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

type Service struct{ store domain.ProgressStore }

var _ domain.ProgressService = (*Service)(nil)

func New(store domain.ProgressStore) *Service { return &Service{store: store} }

func (s *Service) Get(ctx context.Context, userID, versionID string) (domain.Progress, error) {
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) {
		return domain.Progress{}, domain.ErrInvalidProgress
	}
	return s.store.ReadProgress(ctx, userID, versionID)
}

func (s *Service) Put(ctx context.Context, userID, versionID string, command domain.ProgressMutation) (domain.ProgressResult, error) {
	var result domain.ProgressResult
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) || !syncvalue.ValidID(command.ID) || !syncvalue.ValidID(command.DeviceID) ||
		command.BaseRevision < 0 || command.BaseRevision >= domain.MaxMutationRevision ||
		!syncvalue.ValidSHA256(command.Payload.SourceSHA256) || len(command.Payload.Locator) > 16*1024 {
		return result, domain.ErrInvalidProgress
	}
	version, err := s.store.Version(ctx, versionID)
	if err != nil {
		return result, err
	}
	locator, err := normalizeLocator(command.Payload.Locator, version)
	if err != nil {
		return result, domain.ErrInvalidProgress
	}
	command.Payload.Locator = locator
	// Include operation and destination: mutation IDs are global to this user,
	// not interchangeable across versions or future annotation endpoints.
	identity := struct {
		Operation string                  `json:"operation"`
		VersionID string                  `json:"versionID"`
		Command   domain.ProgressMutation `json:"command"`
	}{"progress.put.v1", versionID, command}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(encoded)
	return s.store.CommitProgress(ctx, userID, versionID, command, hex.EncodeToString(digest[:]))
}

func (s *Service) Changes(ctx context.Context, userID string, after int64, limit int) (domain.ChangePage, error) {
	if !syncvalue.ValidID(userID) || after < 0 || limit < 1 || limit > 100 {
		return domain.ChangePage{}, domain.ErrInvalidProgress
	}
	return s.store.ReadChanges(ctx, userID, after, limit)
}

type pdfLocation struct {
	PageIndex   *int64   `json:"pageIndex"`
	Progression *float64 `json:"progression,omitempty"`
}

type locatorEnvelope struct {
	Format domain.Format   `json:"format"`
	EPUB   json.RawMessage `json:"epub,omitempty"`
	PDF    *pdfLocation    `json:"pdf,omitempty"`
}

func normalizeLocator(raw []byte, version domain.VersionDetails) ([]byte, error) {
	canonical, err := jsonvalue.Normalize(raw)
	if err != nil {
		return nil, err
	}
	object, err := jsonvalue.Object(canonical, "format", "epub", "pdf")
	if err != nil {
		return nil, err
	}
	var locator locatorEnvelope
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&locator); err != nil || locator.Format != version.Format {
		return nil, domain.ErrInvalidProgress
	}
	switch locator.Format {
	case domain.PDF:
		if _, err := jsonvalue.Object(object["pdf"], "pageIndex", "progression"); err != nil {
			return nil, err
		}
		if len(locator.EPUB) != 0 || locator.PDF == nil || locator.PDF.PageIndex == nil ||
			version.Metadata.PDF == nil || *locator.PDF.PageIndex < 0 ||
			*locator.PDF.PageIndex >= int64(version.Metadata.PDF.PageCount) || !position.ValidFraction(locator.PDF.Progression) {
			return nil, domain.ErrInvalidProgress
		}
	case domain.EPUB:
		if locator.PDF != nil || len(locator.EPUB) == 0 || !position.ValidEPUBLocator(locator.EPUB) {
			return nil, domain.ErrInvalidProgress
		}
	default:
		return nil, domain.ErrInvalidProgress
	}
	encoded, err := json.Marshal(locator)
	if err != nil {
		return nil, err
	}
	return jsonvalue.Normalize(encoded)
}
