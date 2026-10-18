-- name: SetCurrentProject :exec
-- Transaction-scoped (is_local = true): the setting never leaks to the next
-- user of the pooled connection.
SELECT set_config('app.current_project_id', sqlc.arg(project_id)::text, true);
