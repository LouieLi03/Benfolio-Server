// Package domain defines provider-neutral data boundaries. API capabilities
// describe which reading and annotation operations are implemented.
package domain

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

type Format string

const (
	EPUB Format = "epub"
	PDF  Format = "pdf"
)

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type Library struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	CreatedAt        time.Time `json:"createdAt"`
	SourceConfigured bool      `json:"sourceConfigured"`
}

type Book struct {
	ID               string    `json:"id"`
	LibraryID        string    `json:"libraryID"`
	CurrentVersionID string    `json:"currentVersionID,omitempty"`
	Title            string    `json:"title"`
	Authors          []string  `json:"authors"`
	Format           Format    `json:"format"`
	CreatedAt        time.Time `json:"createdAt"`
}

// DocumentVersion identifies immutable source bytes, not the annotated PDF.
type DocumentVersion struct {
	ID        string    `json:"id"`
	BookID    string    `json:"bookID"`
	SHA256    string    `json:"sha256"`
	ByteSize  int64     `json:"byteSize"`
	CreatedAt time.Time `json:"createdAt"`
}

// PDFWorkingCopy is private to a user and a source version. Its byte hash may
// change on annotation save without changing the identity of the source book.
type PDFWorkingCopy struct {
	UserID                string `json:"userID"`
	DocumentVersionID     string `json:"documentVersionID"`
	Revision              int64  `json:"revision"`
	FileSHA256            string `json:"fileSHA256"`
	MaterializedChangeSeq int64  `json:"materializedChangeSeq"`
}

type TextQuote struct {
	Exact  string `json:"exact"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

type EPUBAnchor struct {
	Href    string          `json:"href"`
	CFI     string          `json:"cfi,omitempty"`
	Quote   TextQuote       `json:"quote"`
	Locator json.RawMessage `json:"locator,omitempty"`
}

// PDF anchors declare their coordinate space. Version-local page indices and
// normalized rectangles precede future stable page IDs and editing mappings.
type PDFAnchor struct {
	PageID          string     `json:"pageID,omitempty"`
	PageIndex       *int64     `json:"pageIndex,omitempty"`
	CoordinateSpace string     `json:"coordinateSpace,omitempty"`
	Rects           []Rect     `json:"rects,omitempty"`
	Quote           *TextQuote `json:"quote,omitempty"`
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Annotation struct {
	ID                string          `json:"id"`
	DocumentVersionID string          `json:"documentVersionID"`
	Kind              string          `json:"kind"`
	EPUB              *EPUBAnchor     `json:"epub,omitempty"`
	PDF               *PDFAnchor      `json:"pdf,omitempty"`
	Body              json.RawMessage `json:"body"`
	Revision          int64           `json:"revision"`
	DeletedAt         *time.Time      `json:"deletedAt,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	DeviceID          string          `json:"deviceID,omitempty"`
}

// Ink is a version-bound portable stroke group. Client conversion and renderer
// fidelity require separate acceptance; this is not serialized PKDrawing.
type Ink struct {
	SchemaVersion     int         `json:"schemaVersion"`
	DocumentVersionID string      `json:"documentVersionID"`
	SourceSHA256      string      `json:"sourceSHA256"`
	PageIndex         *int64      `json:"pageIndex"`
	CoordinateSpace   string      `json:"coordinateSpace"`
	Strokes           []InkStroke `json:"strokes"`
}

type InkStroke struct {
	ID     string     `json:"id"`
	Tool   string     `json:"tool"`
	Color  string     `json:"color"`
	Width  float64    `json:"width"`
	Points []InkPoint `json:"points"`
}

type InkPoint struct {
	X        float64  `json:"x"`
	Y        float64  `json:"y"`
	Pressure *float64 `json:"pressure,omitempty"`
	TimeMS   *float64 `json:"timeMS,omitempty"`
}

type Progress struct {
	DocumentVersionID string          `json:"documentVersionID"`
	Locator           json.RawMessage `json:"locator"`
	Completed         bool            `json:"completed"`
	Revision          int64           `json:"revision"`
	DeviceID          string          `json:"deviceID,omitempty"`
	UpdatedAt         *time.Time      `json:"updatedAt,omitempty"`
}

// Mutation IDs are scoped to a user. Writes must check BaseRevision and store
// the mutation receipt in the same transaction as state and change events.
type Mutation struct {
	ID           string          `json:"id"`
	DeviceID     string          `json:"deviceID"`
	BaseRevision int64           `json:"baseRevision"`
	Payload      json.RawMessage `json:"payload"`
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
