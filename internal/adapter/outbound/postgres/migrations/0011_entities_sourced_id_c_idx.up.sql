-- Lists end in ORDER BY sourced_id COLLATE "C" (bytewise, as the in-memory
-- store orders). The UNIQUE (type, sourced_id) index uses the database
-- collation, so on a non-"C" database it cannot serve that order.
CREATE INDEX IF NOT EXISTS entities_type_sourced_id_c_idx ON entities (type, sourced_id COLLATE "C");
