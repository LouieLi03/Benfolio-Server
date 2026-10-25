ALTER TABLE annotations ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
CREATE INDEX annotations_version_cursor ON annotations(user_id, document_version_id, id);
