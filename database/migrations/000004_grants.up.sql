-- Roles are created by database/bootstrap/002-roles.sql.
GRANT SELECT ON projects TO memory_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON memory_spaces, memories, memory_links TO memory_app;
GRANT EXECUTE ON FUNCTION resolve_api_key(text), touch_api_key(uuid) TO memory_app;
GRANT SELECT ON schema_migrations TO memory_app;

GRANT SELECT, INSERT, UPDATE, DELETE
  ON projects, memory_spaces, memories, memory_links, project_api_keys
  TO memory_admin;
GRANT SELECT ON schema_migrations TO memory_admin;
