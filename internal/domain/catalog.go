package domain

import "context"

type BookQuery struct {
	Query  string
	Format Format
	Limit  int
	Offset int
}

type BookPage struct {
	Items   []Book `json:"items"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
	HasMore bool   `json:"hasMore"`
}

// Catalog provides immutable-version and book reads. Mutations use separate
// interfaces with user identity and revision preconditions.
type Catalog interface {
	Libraries(context.Context) ([]Library, error)
	Books(context.Context, BookQuery) (BookPage, error)
	Book(context.Context, string) (Book, error)
	Versions(context.Context, string, VersionQuery) (VersionPage, error)
	Version(context.Context, string) (VersionDetails, error)
}
