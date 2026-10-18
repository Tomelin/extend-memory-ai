-- name: InsertLink :one
INSERT INTO memory_links (project_id, space_id, source_memory_id, target_memory_id, relation_type, weight, metadata)
VALUES (sqlc.arg(project_id), sqlc.arg(space_id), sqlc.arg(source_memory_id), sqlc.arg(target_memory_id),
        sqlc.arg(relation_type), sqlc.arg(weight), sqlc.arg(metadata))
RETURNING *;

-- name: ListLinks :many
-- Relations of one memory whose other end is still active.
SELECT sqlc.embed(l)
FROM memory_links l
JOIN memories s ON s.id = l.source_memory_id
JOIN memories t ON t.id = l.target_memory_id
WHERE l.space_id = sqlc.arg(space_id)
  AND ((sqlc.arg(outgoing)::boolean AND l.source_memory_id = sqlc.arg(memory_id))
    OR (sqlc.arg(incoming)::boolean AND l.target_memory_id = sqlc.arg(memory_id)))
  AND (sqlc.narg(relation_type)::text IS NULL OR l.relation_type = sqlc.narg(relation_type)::text)
  AND s.deleted_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > now())
  AND t.deleted_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())
ORDER BY l.created_at, l.source_memory_id, l.target_memory_id, l.relation_type;

-- name: ListLinksOfMemories :many
-- Direct relations (both directions) of the recalled memories.
SELECT sqlc.embed(l)
FROM memory_links l
JOIN memories s ON s.id = l.source_memory_id
JOIN memories t ON t.id = l.target_memory_id
WHERE l.space_id = sqlc.arg(space_id)
  AND (l.source_memory_id = ANY (sqlc.arg(memory_ids)::uuid[]) OR l.target_memory_id = ANY (sqlc.arg(memory_ids)::uuid[]))
  AND s.deleted_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > now())
  AND t.deleted_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())
ORDER BY l.created_at, l.source_memory_id, l.target_memory_id, l.relation_type;

-- name: DeleteLink :execrows
DELETE FROM memory_links
WHERE space_id = sqlc.arg(space_id)
  AND source_memory_id = sqlc.arg(source_memory_id)
  AND target_memory_id = sqlc.arg(target_memory_id)
  AND relation_type = sqlc.arg(relation_type);
