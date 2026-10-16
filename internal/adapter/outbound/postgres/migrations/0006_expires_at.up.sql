-- Record expiry for data loaded through POST /api/v1/populate with a ttl.
-- NULL means the record never expires. Reads filter on
-- (expires_at IS NULL OR expires_at > now()); the sweeper deletes rows with
-- expires_at <= now(). The partial indexes keep both cheap, since most rows
-- never expire.
ALTER TABLE entities ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE persons ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS entities_expires_at_idx ON entities (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS persons_expires_at_idx ON persons (expires_at) WHERE expires_at IS NOT NULL;
