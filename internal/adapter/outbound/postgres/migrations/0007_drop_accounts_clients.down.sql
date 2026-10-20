-- Recreate the empty tables of 0002 and 0004. The dropped rows are gone.
CREATE TABLE IF NOT EXISTS accounts (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    roles         JSONB NOT NULL DEFAULT '[]'::jsonb,
    status        TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    oidc_issuer   TEXT NOT NULL DEFAULT '',
    oidc_subject  TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS accounts_oidc_identity_idx
    ON accounts (oidc_issuer, oidc_subject)
    WHERE oidc_subject <> '';

CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id   TEXT PRIMARY KEY,
    secret_hash TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    scopes      JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
