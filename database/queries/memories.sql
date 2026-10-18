-- name: CountMemories :one
-- Live memories visible to the current project (RLS applies).
SELECT count(*)::bigint AS total
FROM memories
WHERE deleted_at IS NULL;

-- name: InsertMemory :one
-- Returns no row when the idempotency key already exists in the space.
INSERT INTO memories (
  project_id, space_id, memory_type, content, summary, embedding, embedding_model,
  source_type, source_ref, idempotency_key, idempotency_hash, importance,
  occurred_at, expires_at, metadata
) VALUES (
  sqlc.arg(project_id), sqlc.arg(space_id), sqlc.arg(memory_type), sqlc.arg(content),
  sqlc.narg(summary), sqlc.narg(embedding), sqlc.narg(embedding_model),
  sqlc.narg(source_type), sqlc.narg(source_ref), sqlc.narg(idempotency_key),
  sqlc.narg(idempotency_hash), sqlc.arg(importance), sqlc.arg(occurred_at),
  sqlc.narg(expires_at), sqlc.arg(metadata)
)
ON CONFLICT (project_id, space_id, idempotency_key) WHERE idempotency_key IS NOT NULL
DO NOTHING
RETURNING id, project_id, space_id, memory_type, content, summary, embedding_model,
  source_type, source_ref, idempotency_key, idempotency_hash, importance,
  occurred_at, expires_at, metadata, created_at, updated_at, deleted_at;

-- name: GetMemoryByIdempotencyKey :one
SELECT id, project_id, space_id, memory_type, content, summary, embedding_model,
  source_type, source_ref, idempotency_key, idempotency_hash, importance,
  occurred_at, expires_at, metadata, created_at, updated_at, deleted_at
FROM memories
WHERE space_id = sqlc.arg(space_id) AND idempotency_key = sqlc.arg(idempotency_key);

-- name: GetActiveMemory :one
SELECT id, project_id, space_id, memory_type, content, summary, embedding_model,
  source_type, source_ref, idempotency_key, idempotency_hash, importance,
  occurred_at, expires_at, metadata, created_at, updated_at, deleted_at
FROM memories
WHERE space_id = sqlc.arg(space_id) AND id = sqlc.arg(id)
  AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now());

-- name: SoftDeleteMemory :execrows
UPDATE memories
SET deleted_at = now()
WHERE space_id = sqlc.arg(space_id) AND id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: RecallLexical :many
-- Full-text candidates ranked by ts_rank_cd, with the optional recall filters.
SELECT id, project_id, space_id, memory_type, content, summary, embedding_model,
  source_type, source_ref, idempotency_key, idempotency_hash, importance,
  occurred_at, expires_at, metadata, created_at, updated_at, deleted_at,
  row_number() OVER (ORDER BY ts_rank_cd(search_vector, q) DESC, occurred_at DESC, id)::int AS rank
FROM memories, websearch_to_tsquery('portuguese_unaccent', sqlc.arg(query)::text) AS q
WHERE space_id = sqlc.arg(space_id)
  AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now())
  AND search_vector @@ q
  AND (sqlc.narg(memory_types)::text[] IS NULL OR memory_type = ANY (sqlc.narg(memory_types)::text[]))
  AND (sqlc.narg(occurred_after)::timestamptz IS NULL OR occurred_at >= sqlc.narg(occurred_after)::timestamptz)
  AND (sqlc.narg(occurred_before)::timestamptz IS NULL OR occurred_at < sqlc.narg(occurred_before)::timestamptz)
  AND (sqlc.narg(source_type)::text IS NULL OR source_type = sqlc.narg(source_type)::text)
  AND (sqlc.narg(source_ref)::text IS NULL OR source_ref = sqlc.narg(source_ref)::text)
  AND (sqlc.narg(metadata)::jsonb IS NULL OR metadata @> sqlc.narg(metadata)::jsonb)
ORDER BY rank
LIMIT sqlc.arg(candidate_limit)::int;

-- name: ListMemories :many
-- Keyset pagination on (occurred_at DESC, id DESC), served by memories_active_timeline_idx.
SELECT id, project_id, space_id, memory_type, content, summary, embedding_model,
  source_type, source_ref, idempotency_key, idempotency_hash, importance,
  occurred_at, expires_at, metadata, created_at, updated_at, deleted_at
FROM memories
WHERE space_id = sqlc.arg(space_id)
  AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now())
  AND (sqlc.narg(memory_types)::text[] IS NULL OR memory_type = ANY (sqlc.narg(memory_types)::text[]))
  AND (sqlc.narg(occurred_after)::timestamptz IS NULL OR occurred_at >= sqlc.narg(occurred_after)::timestamptz)
  AND (sqlc.narg(occurred_before)::timestamptz IS NULL OR occurred_at < sqlc.narg(occurred_before)::timestamptz)
  AND (sqlc.narg(source_type)::text IS NULL OR source_type = sqlc.narg(source_type)::text)
  AND (sqlc.narg(source_ref)::text IS NULL OR source_ref = sqlc.narg(source_ref)::text)
  AND (sqlc.narg(after_occurred_at)::timestamptz IS NULL
       OR (occurred_at, id) < (sqlc.narg(after_occurred_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::int;
