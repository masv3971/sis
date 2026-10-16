DROP INDEX IF EXISTS persons_expires_at_idx;
DROP INDEX IF EXISTS entities_expires_at_idx;
ALTER TABLE persons DROP COLUMN IF EXISTS expires_at;
ALTER TABLE entities DROP COLUMN IF EXISTS expires_at;
