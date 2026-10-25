CREATE TABLE working_pdf_history (
    user_id TEXT NOT NULL REFERENCES users(id),
    document_version_id TEXT NOT NULL REFERENCES document_versions(id),
    revision INTEGER NOT NULL CHECK (revision > 0),
    file_sha256 TEXT NOT NULL REFERENCES resources(sha256),
    materialized_change_seq INTEGER NOT NULL CHECK (materialized_change_seq >= 0),
    annotation_count INTEGER NOT NULL CHECK (annotation_count >= 0),
    device_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, document_version_id, revision)
);

CREATE INDEX changes_version_entity_seq ON changes(user_id, document_version_id, entity_type, seq);
