-- Canonical person/student table. Scalar columns carry the queryable fields;
-- the variable-cardinality attributes (roles, identifiers, contacts) are JSONB
-- so the shape matches the domain model without child-table joins in v1.
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
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS persons_family_given_idx ON persons (family_name, given_name);
