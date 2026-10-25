-- Each immutable row is both the export record and its publication event.
CREATE TABLE export_records (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id),
    id TEXT NOT NULL,
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64),
    book_format TEXT NOT NULL CHECK (book_format IN ('epub', 'pdf')),
    format TEXT NOT NULL CHECK (format IN ('pdf', 'markdown')),
    file_sha256 TEXT NOT NULL REFERENCES resources(sha256),
    annotation_seq INTEGER NOT NULL CHECK (annotation_seq >= 0),
    working_revision INTEGER,
    annotation_count INTEGER NOT NULL CHECK (annotation_count >= 0),
    stale_at_creation INTEGER NOT NULL CHECK (stale_at_creation IN (0, 1)),
    device_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(user_id, id),
    CHECK ((format = 'pdf' AND book_format = 'pdf' AND working_revision IS NOT NULL AND working_revision > 0)
        OR (format = 'markdown' AND working_revision IS NULL)),
    FOREIGN KEY(user_id, document_version_id, working_revision)
        REFERENCES working_pdf_history(user_id, document_version_id, revision)
);

CREATE INDEX exports_version ON export_records(user_id, document_version_id, seq);
