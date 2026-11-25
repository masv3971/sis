-- sourcedId is stored lowercase and printable ASCII only (see domain.FoldSourcedID),
-- so lookups and UNIQUE (type, sourced_id) ignore case. Fail first, naming the
-- rows, instead of letting the UPDATE hit a bare unique violation.
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(format('%s:%s', type, sourced_id), ', ') INTO bad
    FROM entities
    WHERE sourced_id !~ '^[\x20-\x7e]*$';
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'sourcedId must be printable ASCII; fix these (type:sourcedId) first: %', bad;
    END IF;

    SELECT string_agg(format('%s:%s', type, ids), '; ') INTO bad
    FROM (
        SELECT type, string_agg(sourced_id, ' | ' ORDER BY sourced_id) AS ids
        FROM entities
        GROUP BY type, lower(sourced_id)
        HAVING count(*) > 1
    ) collisions;
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'sourcedIds collide ignoring case; merge or rename these (type:ids) first: %', bad;
    END IF;
END $$;

UPDATE entities
SET sourced_id = lower(sourced_id),
    data = jsonb_set(data, '{sourcedId}', to_jsonb(lower(sourced_id)))
WHERE sourced_id <> lower(sourced_id);
