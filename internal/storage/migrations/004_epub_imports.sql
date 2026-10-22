CREATE UNIQUE INDEX books_identity_library ON books(id, library_id);
CREATE UNIQUE INDEX versions_identity_book ON document_versions(id, book_id);

CREATE TABLE book_sources (
    library_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    book_id TEXT NOT NULL UNIQUE,
    current_version_id TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(library_id, relative_path),
    FOREIGN KEY(book_id, library_id) REFERENCES books(id, library_id),
    FOREIGN KEY(current_version_id, book_id) REFERENCES document_versions(id, book_id)
);

CREATE TABLE document_metadata (
    version_id TEXT PRIMARY KEY REFERENCES document_versions(id),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json))
);
