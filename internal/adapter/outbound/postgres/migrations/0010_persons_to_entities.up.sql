-- Persons are stored as documents in entities, like every other type. Before
-- 1.0 nothing is carried over from the old table.
DROP TABLE IF EXISTS persons;

-- Default person order, spelled as DocRepo's docKey and ORDER BY spell it so
-- the planner can use the index.
CREATE INDEX IF NOT EXISTS entities_person_name_idx ON entities (
    (CASE WHEN jsonb_typeof(data->'familyName') = 'string' THEN data->>'familyName' END) COLLATE "C",
    (CASE WHEN jsonb_typeof(data->'givenName') = 'string' THEN data->>'givenName' END) COLLATE "C",
    sourced_id
) WHERE type = 'person';
