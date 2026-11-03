-- A person's enrollments and results (GET /api/v1/persons/:id/enrollments,
-- .../results, and the cascade on person delete) are looked up by the
-- personId inside the JSONB document. Not partial: the type is a query
-- parameter, which a generic plan could not match against a predicate.
CREATE INDEX IF NOT EXISTS entities_type_person_idx ON entities (type, (data->>'personId'));
