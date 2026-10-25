CREATE TABLE ink_resources (
    resource_sha256 TEXT PRIMARY KEY REFERENCES resources(sha256),
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64),
    page_index INTEGER NOT NULL CHECK (page_index >= 0),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    coordinate_space TEXT NOT NULL CHECK (coordinate_space = 'normalized-crop-unrotated-top-left')
);

CREATE TABLE resource_owners (
    user_id TEXT NOT NULL REFERENCES users(id),
    resource_sha256 TEXT NOT NULL REFERENCES resources(sha256),
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, resource_sha256)
);
