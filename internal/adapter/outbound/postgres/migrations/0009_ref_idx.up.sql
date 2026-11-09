-- Serves the Edu-API nested routes: enrollments by offeringId (0008 already
-- indexes that key for every type) and offerings by academicSessionId.
CREATE INDEX IF NOT EXISTS entities_academic_session_id_idx ON entities (type, (data->>'academicSessionId')) WHERE data->>'academicSessionId' IS NOT NULL;
