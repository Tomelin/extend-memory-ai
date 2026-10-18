-- name: Ping :one
SELECT 1::int AS ok;

-- name: SchemaVersion :one
SELECT version, dirty FROM schema_migrations LIMIT 1;

-- name: EmbeddingDimensions :one
-- pgvector stores the declared dimension as the column type modifier.
SELECT a.atttypmod::int AS dimensions
FROM pg_catalog.pg_attribute a
WHERE a.attrelid = to_regclass('public.memories')
  AND a.attname = 'embedding'
  AND NOT a.attisdropped;
