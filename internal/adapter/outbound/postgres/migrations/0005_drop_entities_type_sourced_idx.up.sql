-- UNIQUE (type, sourced_id) on entities already provides a B-tree index on
-- those columns; the separate entities_type_sourced_idx (created by earlier
-- revisions of 0003) only duplicated storage and write cost. 0003 no longer
-- creates it; this drops it from databases that already ran 0003.
DROP INDEX IF EXISTS entities_type_sourced_idx;
