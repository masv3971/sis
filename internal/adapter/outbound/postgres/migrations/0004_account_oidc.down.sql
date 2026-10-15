DROP INDEX IF EXISTS accounts_oidc_identity_idx;
ALTER TABLE accounts DROP COLUMN IF EXISTS oidc_subject;
ALTER TABLE accounts DROP COLUMN IF EXISTS oidc_issuer;
