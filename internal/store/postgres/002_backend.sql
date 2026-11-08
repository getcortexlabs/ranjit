CREATE TABLE connection_requests (
    key_hash TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    expires_at BIGINT NOT NULL
);
CREATE INDEX connection_requests_expiry ON connection_requests(expires_at);

CREATE TABLE webhook_deliveries (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    target_hash TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'delivered', 'failed')),
    attempts BIGINT NOT NULL DEFAULT 0,
    http_status BIGINT NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    available_at BIGINT NOT NULL,
    lease_until BIGINT NOT NULL DEFAULT 0,
    lease_token TEXT NOT NULL DEFAULT '',
    UNIQUE(job_id, event_type)
);
CREATE INDEX webhook_deliveries_available ON webhook_deliveries(target_hash, status, available_at);
CREATE INDEX webhook_deliveries_expiry ON webhook_deliveries(updated_at);
