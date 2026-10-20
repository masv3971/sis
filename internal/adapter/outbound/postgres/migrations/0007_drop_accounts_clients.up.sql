-- SIS no longer issues tokens or stores credentials: access tokens come from
-- trusted issuers (sis.auth.issuers). Drop the local login accounts and
-- OAuth2 clients, with their password and secret hashes.
DROP TABLE IF EXISTS oauth_clients;
DROP TABLE IF EXISTS accounts;
