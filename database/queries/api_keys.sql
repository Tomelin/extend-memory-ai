-- name: InsertAPIKey :one
INSERT INTO project_api_keys (project_id, prefix, secret_hash, name, scopes, expires_at)
VALUES (sqlc.arg(project_id), sqlc.arg(prefix), sqlc.arg(secret_hash), sqlc.arg(name),
        sqlc.arg(scopes)::text[], sqlc.narg(expires_at))
RETURNING id, prefix, name, scopes, expires_at, created_at;

-- name: RevokeAPIKey :execrows
UPDATE project_api_keys
SET revoked_at = now()
WHERE prefix = sqlc.arg(prefix) AND revoked_at IS NULL;

-- name: ListAPIKeys :many
SELECT k.id, k.prefix, k.name, k.scopes, k.expires_at, k.revoked_at, k.last_used_at, k.created_at
FROM project_api_keys k
JOIN projects p ON p.id = k.project_id
WHERE p.slug = sqlc.arg(project_slug)
ORDER BY k.created_at, k.prefix;
