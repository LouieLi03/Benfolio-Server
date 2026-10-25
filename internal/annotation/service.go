// Package annotation handles version-bound text annotations and ink references.
package annotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
	"github.com/LouieLi03/Benfolio-Server/internal/position"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

const normalizedPDFSpace = "normalized-crop-unrotated-top-left"

type Service struct{ store domain.AnnotationStore }

var _ domain.AnnotationService = (*Service)(nil)

func New(store domain.AnnotationStore) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context, userID, versionID, after string, limit int) (domain.AnnotationPage, error) {
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(versionID) ||
		after != "" && !syncvalue.ValidID(after) || limit < 1 || limit > 100 {
		return domain.AnnotationPage{}, domain.ErrInvalidAnnotation
	}
	return s.store.ReadAnnotations(ctx, userID, versionID, after, limit)
}

func (s *Service) Get(ctx context.Context, userID, versionID, annotationID string) (domain.Annotation, error) {
	if !validTarget(userID, versionID, annotationID) {
		return domain.Annotation{}, domain.ErrInvalidAnnotation
	}
	return s.store.ReadAnnotation(ctx, userID, versionID, annotationID)
}

func (s *Service) Put(ctx context.Context, userID, versionID, annotationID string, command domain.AnnotationMutation) (domain.AnnotationResult, error) {
	return s.apply(ctx, userID, versionID, annotationID, command, false)
}

func (s *Service) Delete(ctx context.Context, userID, versionID, annotationID string, command domain.AnnotationMutation) (domain.AnnotationResult, error) {
	return s.apply(ctx, userID, versionID, annotationID, command, true)
}

func validTarget(userID, versionID, annotationID string) bool {
	return syncvalue.ValidID(userID) && syncvalue.ValidID(versionID) && syncvalue.ValidID(annotationID)
}

func (s *Service) apply(ctx context.Context, userID, versionID, annotationID string, command domain.AnnotationMutation, remove bool) (domain.AnnotationResult, error) {
	var result domain.AnnotationResult
	if !validTarget(userID, versionID, annotationID) || !syncvalue.ValidID(command.ID) || !syncvalue.ValidID(command.DeviceID) ||
		command.BaseRevision < 0 || command.BaseRevision >= domain.MaxMutationRevision ||
		!syncvalue.ValidSHA256(command.Payload.SourceSHA256) {
		return result, domain.ErrInvalidAnnotation
	}
	version, err := s.store.Version(ctx, versionID)
	if err != nil {
		return result, err
	}
	operation := "annotation.put.v1"
	if remove {
		operation = "annotation.delete.v1"
		if command.BaseRevision == 0 || command.Payload.Kind != "" || len(command.Payload.EPUB) != 0 ||
			len(command.Payload.PDF) != 0 || len(command.Payload.Body) != 0 {
			return result, domain.ErrInvalidAnnotation
		}
	} else {
		command.Payload, err = normalizePayload(command.Payload, version)
		if err != nil {
			return result, domain.ErrInvalidAnnotation
		}
	}
	identity := struct {
		Operation    string                    `json:"operation"`
		VersionID    string                    `json:"versionID"`
		AnnotationID string                    `json:"annotationID"`
		Command      domain.AnnotationMutation `json:"command"`
	}{operation, versionID, annotationID, command}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(encoded)
	return s.store.CommitAnnotation(ctx, userID, versionID, annotationID, command, remove, hex.EncodeToString(digest[:]))
}

func normalizePayload(payload domain.AnnotationPayload, version domain.VersionDetails) (domain.AnnotationPayload, error) {
	if payload.Kind != "bookmark" && payload.Kind != "highlight" && payload.Kind != "note" && payload.Kind != "ink" {
		return payload, domain.ErrInvalidAnnotation
	}
	var err error
	switch version.Format {
	case domain.EPUB:
		if len(payload.PDF) != 0 || payload.Kind == "ink" {
			return payload, domain.ErrInvalidAnnotation
		}
		payload.EPUB, err = normalizeEPUB(payload.EPUB, payload.Kind)
	case domain.PDF:
		if len(payload.EPUB) != 0 || version.Metadata.PDF == nil {
			return payload, domain.ErrInvalidAnnotation
		}
		payload.PDF, err = normalizePDF(payload.PDF, payload.Kind, version.Metadata.PDF.PageCount)
	default:
		err = domain.ErrInvalidAnnotation
	}
	if err != nil {
		return payload, err
	}
	payload.Body, err = normalizeBody(payload.Body, payload.Kind)
	return payload, err
}

func normalizedObject(raw []byte, fields ...string) ([]byte, map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 16*1024 {
		return nil, nil, domain.ErrInvalidAnnotation
	}
	canonical, err := jsonvalue.Normalize(raw)
	if err != nil || len(canonical) > 16*1024 {
		return nil, nil, domain.ErrInvalidAnnotation
	}
	object, err := jsonvalue.Object(canonical, fields...)
	return canonical, object, err
}

func normalizeEPUB(raw []byte, kind string) ([]byte, error) {
	canonical, object, err := normalizedObject(raw, "href", "cfi", "quote", "locator")
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"href", "cfi"} {
		if value, exists := object[key]; exists {
			if _, err := plainString(value); err != nil {
				return nil, err
			}
		}
	}
	var anchor domain.EPUBAnchor
	if json.Unmarshal(canonical, &anchor) != nil || !position.ValidEPUBHref(anchor.Href) || len(anchor.CFI) > 4096 {
		return nil, domain.ErrInvalidAnnotation
	}
	if quote, exists := object["quote"]; exists {
		if err := validateQuote(quote); err != nil {
			return nil, err
		}
	}
	if kind == "highlight" && strings.TrimSpace(anchor.Quote.Exact) == "" {
		return nil, domain.ErrInvalidAnnotation
	}
	if len(anchor.Locator) != 0 {
		if !position.ValidEPUBLocator(anchor.Locator) {
			return nil, domain.ErrInvalidAnnotation
		}
		locator, err := jsonvalue.Object(anchor.Locator)
		if err != nil {
			return nil, err
		}
		href, err := plainString(locator["href"])
		if err != nil || href != anchor.Href {
			return nil, domain.ErrInvalidAnnotation
		}
	}
	encoded, err := json.Marshal(anchor)
	if err != nil {
		return nil, err
	}
	normalized, _, err := normalizedObject(encoded)
	return normalized, err
}

func normalizePDF(raw []byte, kind string, pageCount int) ([]byte, error) {
	canonical, object, err := normalizedObject(raw, "pageIndex", "coordinateSpace", "rects", "quote")
	if err != nil {
		return nil, err
	}
	var anchor domain.PDFAnchor
	if json.Unmarshal(canonical, &anchor) != nil || anchor.PageIndex == nil || *anchor.PageIndex < 0 ||
		*anchor.PageIndex >= int64(pageCount) || len(anchor.Rects) > 128 {
		return nil, domain.ErrInvalidAnnotation
	}
	if rects, exists := object["rects"]; exists && string(rects) == "null" {
		return nil, domain.ErrInvalidAnnotation
	}
	if space, exists := object["coordinateSpace"]; exists {
		value, err := plainString(space)
		if err != nil || value != normalizedPDFSpace {
			return nil, domain.ErrInvalidAnnotation
		}
	}
	if quote, exists := object["quote"]; exists {
		if err := validateQuote(quote); err != nil {
			return nil, err
		}
	}
	if kind == "highlight" && len(anchor.Rects) == 0 || kind == "bookmark" && len(anchor.Rects) != 0 {
		return nil, domain.ErrInvalidAnnotation
	}
	if kind == "ink" && (len(anchor.Rects) != 0 || anchor.Quote != nil) {
		return nil, domain.ErrInvalidAnnotation
	}
	if len(anchor.Rects) != 0 {
		if anchor.CoordinateSpace != normalizedPDFSpace {
			return nil, domain.ErrInvalidAnnotation
		}
		var rectangles []json.RawMessage
		if json.Unmarshal(object["rects"], &rectangles) != nil {
			return nil, domain.ErrInvalidAnnotation
		}
		for i, rect := range anchor.Rects {
			fields, err := jsonvalue.Object(rectangles[i], "x", "y", "width", "height")
			if err != nil || len(fields) != 4 {
				return nil, domain.ErrInvalidAnnotation
			}
			for _, value := range fields {
				if string(value) == "null" {
					return nil, domain.ErrInvalidAnnotation
				}
			}
			if !finite(rect.X, rect.Y, rect.Width, rect.Height) || rect.X < 0 || rect.Y < 0 ||
				rect.Width <= 0 || rect.Height <= 0 || rect.X+rect.Width > 1 || rect.Y+rect.Height > 1 {
				return nil, domain.ErrInvalidAnnotation
			}
		}
	} else {
		anchor.CoordinateSpace = ""
	}
	encoded, err := json.Marshal(anchor)
	if err != nil {
		return nil, err
	}
	normalized, _, err := normalizedObject(encoded)
	return normalized, err
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func validateQuote(raw []byte) error {
	fields, err := jsonvalue.Object(raw, "exact", "prefix", "suffix")
	if err != nil {
		return err
	}
	for key, value := range fields {
		text, err := plainString(value)
		maxBytes := 2048
		if key == "exact" {
			maxBytes = 8192
		}
		if err != nil || len(text) > maxBytes {
			return domain.ErrInvalidAnnotation
		}
	}
	return nil
}

func plainString(raw []byte) (string, error) {
	var value *string
	if json.Unmarshal(raw, &value) != nil || value == nil || strings.ContainsRune(*value, 0) {
		return "", domain.ErrInvalidAnnotation
	}
	return *value, nil
}

func normalizeBody(raw []byte, kind string) ([]byte, error) {
	if kind == "ink" {
		canonical, object, err := normalizedObject(raw, "resourceSHA256")
		if err != nil {
			return nil, err
		}
		digest, err := plainString(object["resourceSHA256"])
		if err != nil || !syncvalue.ValidSHA256(digest) {
			return nil, domain.ErrInvalidAnnotation
		}
		return canonical, nil
	}
	_, object, err := normalizedObject(raw, "text", "color")
	if err != nil {
		return nil, err
	}
	body := make(map[string]string)
	for key, raw := range object {
		value, err := plainString(raw)
		if err != nil || len(value) > 16*1024 {
			return nil, domain.ErrInvalidAnnotation
		}
		if key == "color" {
			if kind != "highlight" || len(value) != 7 || value[0] != '#' {
				return nil, domain.ErrInvalidAnnotation
			}
			if _, err := hex.DecodeString(value[1:]); err != nil {
				return nil, domain.ErrInvalidAnnotation
			}
			value = strings.ToUpper(value)
		}
		body[key] = value
	}
	if kind == "note" && strings.TrimSpace(body["text"]) == "" {
		return nil, domain.ErrInvalidAnnotation
	}
	return json.Marshal(body)
}
