-- API keys are resolved before the project is known, so this table has no RLS.
-- The application role has no direct access; it uses the SECURITY DEFINER
-- functions below, which run with the privileges of the table owner.
CREATE TABLE project_api_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  prefix varchar(16) NOT NULL UNIQUE,
  secret_hash bytea NOT NULL,
  name text NOT NULL,
  scopes text[] NOT NULL,
  expires_at timestamptz,
  revoked_at timestamptz,
  last_used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT project_api_keys_name_not_blank CHECK (btrim(name) <> ''),
  CONSTRAINT project_api_keys_scopes_valid CHECK (
    cardinality(scopes) > 0
    AND scopes <@ ARRAY['memories:read', 'memories:write', 'spaces:write']::text[]
  ),
  CONSTRAINT project_api_keys_secret_hash_length CHECK (octet_length(secret_hash) = 32)
);

CREATE INDEX project_api_keys_project_idx ON project_api_keys (project_id);

CREATE FUNCTION resolve_api_key(p_prefix text)
RETURNS SETOF project_api_keys
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  SELECT k.*
  FROM project_api_keys k
  WHERE k.prefix = p_prefix
    AND k.revoked_at IS NULL
    AND (k.expires_at IS NULL OR k.expires_at > now());
$$;

CREATE FUNCTION touch_api_key(p_key_id uuid)
RETURNS void
LANGUAGE sql
VOLATILE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  UPDATE project_api_keys SET last_used_at = now() WHERE id = p_key_id;
$$;

REVOKE ALL ON FUNCTION resolve_api_key(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION touch_api_key(uuid) FROM PUBLIC;
