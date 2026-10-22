-- Absolute mount paths remain private to the server. Library API responses
-- expose stable identity and configuration status, never the source path.
CREATE TABLE library_sources (
    library_id TEXT PRIMARY KEY REFERENCES libraries(id),
    root_path TEXT NOT NULL UNIQUE CHECK (length(root_path) > 0),
    created_at TEXT NOT NULL
);
