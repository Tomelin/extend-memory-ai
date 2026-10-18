-- name: PurgeExpiredMemories :execrows
-- Run by memory-admin (BYPASSRLS) across all projects, in batches to keep
-- locks short. Links go with ON DELETE CASCADE.
DELETE FROM memories
WHERE id IN (
  SELECT id FROM memories
  WHERE expires_at IS NOT NULL AND expires_at <= now()
  LIMIT sqlc.arg(batch_size)::int
);

-- name: PurgeDeletedMemories :execrows
DELETE FROM memories
WHERE id IN (
  SELECT id FROM memories
  WHERE deleted_at IS NOT NULL AND deleted_at < sqlc.arg(deleted_before)::timestamptz
  LIMIT sqlc.arg(batch_size)::int
);
