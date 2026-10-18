-- name: InsertSpace :one
-- Returns no row when the space already exists; the caller then updates it.
INSERT INTO memory_spaces (project_id, external_id, name, kind, metadata)
VALUES (sqlc.arg(project_id), sqlc.arg(external_id), sqlc.arg(name), sqlc.arg(kind), sqlc.arg(metadata))
ON CONFLICT (project_id, external_id) DO NOTHING
RETURNING *;

-- name: UpdateSpace :one
UPDATE memory_spaces
SET name = sqlc.arg(name), kind = sqlc.arg(kind), metadata = sqlc.arg(metadata)
WHERE external_id = sqlc.arg(external_id)
RETURNING *;

-- name: GetSpaceByExternalID :one
SELECT * FROM memory_spaces
WHERE external_id = sqlc.arg(external_id);

-- name: DeleteSpace :execrows
-- Physical removal; memories and links go with ON DELETE CASCADE.
DELETE FROM memory_spaces
WHERE external_id = sqlc.arg(external_id);
