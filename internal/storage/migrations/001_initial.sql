CREATE TABLE server_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role = 'owner'),
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX single_owner ON users(role);

CREATE TABLE libraries (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE books (
    id TEXT PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id),
    title TEXT NOT NULL,
    authors_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(authors_json)),
    format TEXT NOT NULL CHECK (format IN ('epub', 'pdf')),
    created_at TEXT NOT NULL
);
CREATE INDEX books_library ON books(library_id, id);

CREATE TABLE document_versions (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL REFERENCES books(id),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
    byte_size INTEGER NOT NULL CHECK (byte_size > 0),
    original_object_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(book_id, sha256)
);

CREATE TABLE document_pages (
    id TEXT PRIMARY KEY,
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    source_index INTEGER NOT NULL CHECK (source_index >= 0),
    width_points REAL NOT NULL CHECK (width_points > 0),
    height_points REAL NOT NULL CHECK (height_points > 0),
    rotation INTEGER NOT NULL CHECK (rotation IN (0, 90, 180, 270)),
    UNIQUE(document_version_id, source_index)
);

CREATE TABLE reading_progress (
    user_id TEXT NOT NULL REFERENCES users(id),
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    locator_json TEXT NOT NULL CHECK (json_valid(locator_json)),
    completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0, 1)),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, document_version_id)
);

CREATE TABLE annotations (
    id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id),
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    kind TEXT NOT NULL CHECK (kind IN ('bookmark', 'highlight', 'note', 'ink')),
    anchor_json TEXT NOT NULL CHECK (json_valid(anchor_json)),
    body_json TEXT NOT NULL CHECK (json_valid(body_json)),
    revision INTEGER NOT NULL CHECK (revision > 0),
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, id)
);
CREATE INDEX annotations_version ON annotations(user_id, document_version_id);

CREATE TABLE resources (
    sha256 TEXT PRIMARY KEY CHECK (length(sha256) = 64),
    byte_size INTEGER NOT NULL CHECK (byte_size > 0),
    media_type TEXT NOT NULL,
    object_key TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE annotation_resources (
    user_id TEXT NOT NULL,
    annotation_id TEXT NOT NULL,
    resource_sha256 TEXT NOT NULL REFERENCES resources(sha256),
    PRIMARY KEY(user_id, annotation_id, resource_sha256),
    FOREIGN KEY(user_id, annotation_id) REFERENCES annotations(user_id, id)
);

-- A source version never changes when notes change. A working PDF is a
-- per-user materialization of annotations, not a replacement for the source.
CREATE TABLE pdf_working_copies (
    user_id TEXT NOT NULL REFERENCES users(id),
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    file_sha256 TEXT NOT NULL REFERENCES resources(sha256),
    materialized_change_seq INTEGER NOT NULL CHECK (materialized_change_seq >= 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, document_version_id)
);

-- Future mutation handling must commit state, events and receipts together.
-- Tombstones and receipts have no automatic expiry in the initial schema.
CREATE TABLE changes (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id),
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('progress', 'annotation', 'working_pdf')),
    entity_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    operation TEXT NOT NULL CHECK (operation IN ('upsert', 'delete')),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    created_at TEXT NOT NULL
);
CREATE INDEX changes_user_seq ON changes(user_id, seq);

CREATE TABLE mutation_receipts (
    user_id TEXT NOT NULL REFERENCES users(id),
    mutation_id TEXT NOT NULL,
    request_sha256 TEXT NOT NULL CHECK (length(request_sha256) = 64),
    response_json TEXT NOT NULL CHECK (json_valid(response_json)),
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, mutation_id)
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    user_id TEXT REFERENCES users(id),
    kind TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed')),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
