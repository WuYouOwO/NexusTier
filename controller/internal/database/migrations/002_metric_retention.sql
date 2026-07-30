ALTER TABLE telemetry_collection_runs
    ADD COLUMN raw_payload_sha256 char(64),
    ADD CONSTRAINT telemetry_collection_runs_payload_sha256_check
        CHECK (raw_payload_sha256 IS NULL OR raw_payload_sha256 ~ '^[0-9a-f]{64}$'),
    ADD COLUMN payload_pruned_at timestamptz;

CREATE INDEX telemetry_collection_runs_payload_retention_idx
    ON telemetry_collection_runs(completed_at, collection_id)
    WHERE payload_pruned_at IS NULL AND raw_payload_sha256 IS NOT NULL;