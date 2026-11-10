-- Serves ListByRef/ListByRefs on results' offeringId (OEAPI associations
-- nest the person's result per offering). Partial: only results have it.
CREATE INDEX IF NOT EXISTS entities_offering_id_idx ON entities (type, (data->>'offeringId')) WHERE data->>'offeringId' IS NOT NULL;
