-- sourcedId is stored lowercase and printable ASCII only (domain.FoldSourcedID).
-- The folding runs in Go, not here: lower() follows the database locale
-- (Turkish turns I into a dotless i), Go's strings.ToLower does not. See
-- foldSourcedIDs in migrate_sourced_id.go, which the runner calls after this file.
SELECT 1;
