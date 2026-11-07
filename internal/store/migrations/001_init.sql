CREATE TABLE connections (
    id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    institution_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider = 'sandbox'),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'denied', 'expired', 'revoked')),
    created_at INTEGER NOT NULL,
    consent_expires_at INTEGER NOT NULL,
    last_synced_at INTEGER,
    token_hash TEXT UNIQUE,
    authorization_expires_at INTEGER NOT NULL
);
CREATE INDEX connections_customer ON connections(customer_id, id);

CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    balance_minor INTEGER NOT NULL,
    currency TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(connection_id, external_id)
);

CREATE TABLE transactions (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    description TEXT NOT NULL,
    amount_minor INTEGER NOT NULL,
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
    attempts INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    available_at INTEGER NOT NULL,
    lease_until INTEGER NOT NULL DEFAULT 0,
    lease_token TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX jobs_one_active ON jobs(connection_id) WHERE status IN ('queued', 'running');
CREATE INDEX jobs_available ON jobs(status, available_at, lease_until);
