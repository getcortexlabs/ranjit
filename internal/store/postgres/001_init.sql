CREATE TABLE connections (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    institution_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('sandbox', 'efi', 'inter', 'sicoob', 'c6', 'santander', 'file')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'denied', 'expired', 'revoked', 'paused')),
    created_at BIGINT NOT NULL,
    consent_expires_at BIGINT NOT NULL,
    last_synced_at BIGINT,
    token_hash TEXT UNIQUE,
    authorization_expires_at BIGINT NOT NULL,
    authorization_mode TEXT NOT NULL DEFAULT 'sandbox_consent',
    credential_id TEXT NOT NULL DEFAULT '',
    CHECK (
        (provider = 'sandbox' AND authorization_mode = 'sandbox_consent' AND credential_id = '')
        OR (provider = 'file' AND authorization_mode = 'file_import' AND credential_id <> '')
        OR (provider IN ('efi', 'inter', 'sicoob', 'c6', 'santander') AND authorization_mode = 'api_credentials' AND credential_id <> '')
    )
);
CREATE INDEX connections_customer ON connections(customer_id, id);

CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    balance_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    balance_known BIGINT NOT NULL DEFAULT 1 CHECK (balance_known IN (0,1)),
    balance_at BIGINT NOT NULL DEFAULT 0,
    UNIQUE(connection_id, external_id)
);

CREATE TABLE transactions (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    description TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency TEXT NOT NULL,
    posted_on TEXT NOT NULL,
    status TEXT NOT NULL,
    UNIQUE(account_id, external_id)
);
CREATE INDEX transactions_account ON transactions(account_id, id);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    attempts BIGINT NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    available_at BIGINT NOT NULL,
    lease_until BIGINT NOT NULL DEFAULT 0,
    lease_token TEXT NOT NULL DEFAULT '',
    requests_completed BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX jobs_one_active ON jobs(connection_id) WHERE status IN ('queued', 'running');
CREATE INDEX jobs_available ON jobs(status, available_at, lease_until);

CREATE TABLE sync_responses (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    request_hash TEXT NOT NULL,
    body BYTEA NOT NULL,
    received_at BIGINT NOT NULL,
    PRIMARY KEY (job_id, request_hash)
);
CREATE TABLE imports (
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    file_hash TEXT NOT NULL,
    date_from TEXT NOT NULL,
    date_through TEXT NOT NULL,
    stable_ids BIGINT NOT NULL CHECK (stable_ids IN (0, 1)),
    imported_at BIGINT NOT NULL,
    PRIMARY KEY (connection_id, file_hash)
);

CREATE FUNCTION cleanup_finished_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('succeeded', 'failed', 'cancelled') THEN
        DELETE FROM sync_responses WHERE job_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER sync_responses_finished AFTER UPDATE OF status ON jobs
FOR EACH ROW EXECUTE FUNCTION cleanup_finished_job();

CREATE FUNCTION cleanup_ended_connection() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IN ('expired', 'revoked', 'paused') THEN
        DELETE FROM imports WHERE connection_id = NEW.id AND (NEW.provider <> 'file' OR NEW.status = 'revoked');
        DELETE FROM sync_responses WHERE job_id IN (SELECT id FROM jobs WHERE connection_id = NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER imports_revoked AFTER UPDATE OF status ON connections
FOR EACH ROW EXECUTE FUNCTION cleanup_ended_connection();
