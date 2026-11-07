CREATE TABLE connections_next (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    institution_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('sandbox', 'efi', 'inter', 'sicoob', 'c6', 'santander', 'file')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'denied', 'expired', 'revoked')),
    created_at INTEGER NOT NULL,
    consent_expires_at INTEGER NOT NULL,
    last_synced_at INTEGER,
    token_hash TEXT UNIQUE,
    authorization_expires_at INTEGER NOT NULL,
    authorization_mode TEXT NOT NULL DEFAULT 'sandbox_consent',
    credential_id TEXT NOT NULL DEFAULT '',
    CHECK (
        (provider = 'sandbox' AND authorization_mode = 'sandbox_consent' AND credential_id = '')
        OR (provider = 'file' AND authorization_mode = 'file_import' AND credential_id <> '')
        OR (provider IN ('efi', 'inter', 'sicoob', 'c6', 'santander') AND authorization_mode = 'api_credentials' AND credential_id <> '')
    )
);
INSERT INTO connections_next (
    id, customer_id, institution_id, provider, status, created_at,
    consent_expires_at, last_synced_at, token_hash, authorization_expires_at, authorization_mode, credential_id
)
SELECT id, customer_id, institution_id, provider, status, created_at,
       consent_expires_at, last_synced_at, token_hash, authorization_expires_at, authorization_mode, credential_id
FROM connections;
DROP TABLE connections;
ALTER TABLE connections_next RENAME TO connections;
CREATE INDEX connections_customer ON connections(customer_id, id);

ALTER TABLE accounts ADD COLUMN balance_known INTEGER NOT NULL DEFAULT 1 CHECK (balance_known IN (0, 1));
ALTER TABLE accounts ADD COLUMN balance_at INTEGER NOT NULL DEFAULT 0;
UPDATE accounts SET balance_at = updated_at;
ALTER TABLE jobs ADD COLUMN requests_completed INTEGER NOT NULL DEFAULT 0;
CREATE TABLE sync_responses (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    request_hash TEXT NOT NULL,
    body BLOB NOT NULL,
    received_at INTEGER NOT NULL,
    PRIMARY KEY (job_id, request_hash)
);
CREATE TABLE imports (
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    file_hash TEXT NOT NULL,
    date_from TEXT NOT NULL,
    date_through TEXT NOT NULL,
    stable_ids INTEGER NOT NULL CHECK (stable_ids IN (0, 1)),
    imported_at INTEGER NOT NULL,
    PRIMARY KEY (connection_id, file_hash)
);
CREATE TRIGGER sync_responses_finished AFTER UPDATE OF status ON jobs
WHEN NEW.status IN ('succeeded', 'failed', 'cancelled') BEGIN
    DELETE FROM sync_responses WHERE job_id = NEW.id;
END;
CREATE TRIGGER imports_revoked AFTER UPDATE OF status ON connections
WHEN NEW.status IN ('expired', 'revoked') BEGIN
    DELETE FROM imports WHERE connection_id = NEW.id;
    DELETE FROM sync_responses WHERE job_id IN (SELECT id FROM jobs WHERE connection_id = NEW.id);
END;
