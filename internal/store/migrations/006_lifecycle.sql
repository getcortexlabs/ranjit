CREATE TABLE connections_next (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    institution_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('sandbox', 'efi', 'inter', 'sicoob', 'c6', 'santander', 'file')),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'denied', 'expired', 'revoked', 'paused')),
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

CREATE TRIGGER imports_revoked AFTER UPDATE OF status ON connections
WHEN NEW.status IN ('expired', 'revoked', 'paused') BEGIN
    DELETE FROM imports WHERE connection_id = NEW.id AND (NEW.provider <> 'file' OR NEW.status = 'revoked');
    DELETE FROM sync_responses WHERE job_id IN (SELECT id FROM jobs WHERE connection_id = NEW.id);
END;
