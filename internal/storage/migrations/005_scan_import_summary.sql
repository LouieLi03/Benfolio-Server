ALTER TABLE tasks ADD COLUMN import_summary_json TEXT
    CHECK (import_summary_json IS NULL OR json_valid(import_summary_json));
