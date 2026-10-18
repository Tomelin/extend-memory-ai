-- name: ResolveAPIKey :one
SELECT id, project_id, secret_hash, scopes
FROM resolve_api_key(sqlc.arg(prefix)::text);

-- name: TouchAPIKey :exec
SELECT touch_api_key(sqlc.arg(key_id)::uuid);
