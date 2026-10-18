REVOKE SELECT, INSERT, UPDATE, DELETE
  ON projects, memory_spaces, memories, memory_links, project_api_keys
  FROM memory_admin;

REVOKE EXECUTE ON FUNCTION resolve_api_key(text), touch_api_key(uuid) FROM memory_app;
REVOKE SELECT, INSERT, UPDATE, DELETE ON memory_spaces, memories, memory_links FROM memory_app;
REVOKE SELECT ON projects FROM memory_app;
