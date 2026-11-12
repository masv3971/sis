DROP INDEX IF EXISTS entities_person_name_idx;

CREATE TABLE IF NOT EXISTS persons (
    id            TEXT PRIMARY KEY,
    sourced_id    TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL,
    given_name    TEXT NOT NULL,
    family_name   TEXT NOT NULL,
    middle_name   TEXT NOT NULL DEFAULT '',
    date_of_birth TEXT NOT NULL DEFAULT '',
    roles         JSONB NOT NULL DEFAULT '[]'::jsonb,
    identifiers   JSONB NOT NULL DEFAULT '[]'::jsonb,
    emails        JSONB NOT NULL DEFAULT '[]'::jsonb,
    phones        JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    expires_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS persons_family_given_idx ON persons (family_name, given_name);
CREATE INDEX IF NOT EXISTS persons_expires_at_idx ON persons (expires_at) WHERE expires_at IS NOT NULL;
