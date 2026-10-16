-- Bind OIDC accounts to the provider identity (issuer + subject) instead of
-- matching them to local accounts by email. Local accounts keep both columns
-- empty; the partial unique index only covers OIDC-bound rows.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS oidc_issuer  TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS oidc_subject TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS accounts_oidc_identity_idx
    ON accounts (oidc_issuer, oidc_subject)
    WHERE oidc_subject <> '';
