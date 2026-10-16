-- Generic document store for canonical entities beyond Person. Each row is one
-- entity serialized to JSONB, with id/sourced_id/timestamps promoted to columns
-- for keys and ordering. Hot entities can be normalized into their own tables
-- later without changing the domain or service layer.
CREATE TABLE IF NOT EXISTS entities (
    type       TEXT NOT NULL,
    id         TEXT NOT NULL,
    sourced_id TEXT NOT NULL,
    data       JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (type, id),
    -- Also serves as the (type, sourced_id) lookup index.
    UNIQUE (type, sourced_id)
);
