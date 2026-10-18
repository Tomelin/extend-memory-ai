-- name: GetProject :one
SELECT id, slug, name, description
FROM projects
WHERE id = sqlc.arg(id);

-- name: InsertProject :one
INSERT INTO projects (slug, name, description)
VALUES (sqlc.arg(slug), sqlc.arg(name), sqlc.narg(description))
RETURNING id, slug, name, description, created_at;

-- name: GetProjectBySlug :one
SELECT id, slug, name, description
FROM projects
WHERE slug = sqlc.arg(slug);
