CREATE TABLE webhook_deliveries_next (
    id TEXT PRIMARY KEY,
    job_id TEXT REFERENCES jobs(id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    target_hash TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'delivered', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    http_status INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    available_at INTEGER NOT NULL,
    lease_until INTEGER NOT NULL DEFAULT 0,
    lease_token TEXT NOT NULL DEFAULT '',
    UNIQUE(job_id, event_type)
);
INSERT INTO webhook_deliveries_next SELECT * FROM webhook_deliveries;
DROP TABLE webhook_deliveries;
ALTER TABLE webhook_deliveries_next RENAME TO webhook_deliveries;
CREATE INDEX webhook_deliveries_available ON webhook_deliveries(target_hash, status, available_at);
CREATE INDEX webhook_deliveries_expiry ON webhook_deliveries(updated_at);
