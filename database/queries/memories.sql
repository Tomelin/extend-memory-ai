-- name: CountMemories :one
-- Live memories visible to the current project (RLS applies).
SELECT count(*)::bigint AS total
FROM memories
WHERE deleted_at IS NULL;
