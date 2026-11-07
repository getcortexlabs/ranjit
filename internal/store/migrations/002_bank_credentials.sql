CREATE TABLE connections_next (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    institution_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('sandbox', 'efi')),
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
        OR (provider = 'efi' AND authorization_mode = 'api_credentials' AND credential_id <> '')
    )
);
INSERT INTO connections_next (
    id, customer_id, institution_id, provider, status, created_at,
    consent_expires_at, last_synced_at, token_hash, authorization_expires_at
)
SELECT id, customer_id, institution_id, provider, status, created_at,
       consent_expires_at, last_synced_at, token_hash, authorization_expires_at
FROM connections;
DROP TABLE connections;
ALTER TABLE connections_next RENAME TO connections;
CREATE INDEX connections_customer ON connections(customer_id, id);
