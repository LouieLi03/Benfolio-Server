ALTER TABLE tasks ADD COLUMN library_id TEXT REFERENCES libraries(id);
ALTER TABLE tasks ADD COLUMN candidate_files INTEGER NOT NULL DEFAULT 0 CHECK (candidate_files >= 0);
ALTER TABLE tasks ADD COLUMN result_json TEXT CHECK (result_json IS NULL OR json_valid(result_json));
ALTER TABLE tasks ADD COLUMN error_code TEXT;
ALTER TABLE tasks ADD COLUMN error_message TEXT;
ALTER TABLE tasks ADD COLUMN started_at TEXT;
ALTER TABLE tasks ADD COLUMN finished_at TEXT;

CREATE UNIQUE INDEX one_active_source_scan ON tasks(user_id, library_id)
    WHERE kind = 'source_scan' AND state IN ('queued', 'running');
CREATE INDEX queued_source_scans ON tasks(user_id, kind, state, created_at, id);
