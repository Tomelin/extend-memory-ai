-- Text search configuration and core multi-project memory schema.

-- Portuguese stemming with accents removed before tokenization.
CREATE TEXT SEARCH CONFIGURATION portuguese_unaccent (COPY = portuguese);

ALTER TEXT SEARCH CONFIGURATION portuguese_unaccent
  ALTER MAPPING FOR hword, hword_part, word
  WITH unaccent, portuguese_stem;

CREATE TABLE projects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug varchar(100) NOT NULL UNIQUE,
  name text NOT NULL,
  description text,
  settings jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT projects_slug_format CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
  CONSTRAINT projects_name_not_blank CHECK (btrim(name) <> ''),
  CONSTRAINT projects_settings_is_object CHECK (jsonb_typeof(settings) = 'object')
);

CREATE TABLE memory_spaces (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  external_id varchar(255) NOT NULL,
  name text NOT NULL,
  kind varchar(30) NOT NULL DEFAULT 'custom',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT memory_spaces_project_external_id_unique UNIQUE (project_id, external_id),
  CONSTRAINT memory_spaces_project_id_id_unique UNIQUE (project_id, id),
  CONSTRAINT memory_spaces_external_id_not_blank CHECK (btrim(external_id) <> ''),
  CONSTRAINT memory_spaces_name_not_blank CHECK (btrim(name) <> ''),
  CONSTRAINT memory_spaces_kind_valid CHECK (kind IN ('user', 'agent', 'team', 'session', 'custom')),
  CONSTRAINT memory_spaces_metadata_is_object CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE memories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  space_id uuid NOT NULL,
  memory_type varchar(30) NOT NULL DEFAULT 'fact',
  content text NOT NULL,
  summary text,
  embedding vector(1536),
  embedding_model text,
  search_vector tsvector GENERATED ALWAYS AS (
    to_tsvector(
      'portuguese_unaccent',
      coalesce(summary, '') || ' ' || content
    )
  ) STORED,
  source_type varchar(50),
  source_ref text,
  idempotency_key varchar(255),
  importance real NOT NULL DEFAULT 0.5,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CONSTRAINT memories_space_fk
    FOREIGN KEY (project_id, space_id)
    REFERENCES memory_spaces(project_id, id)
    ON DELETE CASCADE,
  CONSTRAINT memories_project_space_id_unique UNIQUE (project_id, space_id, id),
  CONSTRAINT memories_type_valid CHECK (
    memory_type IN ('fact', 'experience', 'preference', 'decision', 'observation', 'summary', 'context')
  ),
  CONSTRAINT memories_content_not_blank CHECK (btrim(content) <> ''),
  CONSTRAINT memories_importance_valid CHECK (importance BETWEEN 0.0 AND 1.0),
  CONSTRAINT memories_expiration_valid CHECK (expires_at IS NULL OR expires_at > occurred_at),
  CONSTRAINT memories_embedding_model_present CHECK (embedding IS NULL OR embedding_model IS NOT NULL),
  CONSTRAINT memories_metadata_is_object CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE TABLE memory_links (
  project_id uuid NOT NULL,
  space_id uuid NOT NULL,
  source_memory_id uuid NOT NULL,
  target_memory_id uuid NOT NULL,
  relation_type varchar(30) NOT NULL,
  weight real NOT NULL DEFAULT 1.0,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (source_memory_id, target_memory_id, relation_type),
  CONSTRAINT memory_links_source_fk
    FOREIGN KEY (project_id, space_id, source_memory_id)
    REFERENCES memories(project_id, space_id, id)
    ON DELETE CASCADE,
  CONSTRAINT memory_links_target_fk
    FOREIGN KEY (project_id, space_id, target_memory_id)
    REFERENCES memories(project_id, space_id, id)
    ON DELETE CASCADE,
  CONSTRAINT memory_links_relation_type_valid CHECK (
    relation_type IN ('related_to', 'supports', 'contradicts', 'supersedes', 'derived_from')
  ),
  CONSTRAINT memory_links_distinct_memories CHECK (source_memory_id <> target_memory_id),
  CONSTRAINT memory_links_weight_valid CHECK (weight BETWEEN 0.0 AND 1.0),
  CONSTRAINT memory_links_metadata_is_object CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE UNIQUE INDEX memories_idempotency_key_unique
  ON memories (project_id, space_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE INDEX memories_active_timeline_idx
  ON memories (project_id, space_id, occurred_at DESC)
  WHERE deleted_at IS NULL;

CREATE INDEX memories_active_search_vector_idx
  ON memories USING gin (search_vector)
  WHERE deleted_at IS NULL;

CREATE INDEX memories_active_source_idx
  ON memories (project_id, space_id, source_type, source_ref)
  WHERE deleted_at IS NULL AND source_ref IS NOT NULL;

CREATE INDEX memory_links_target_idx
  ON memory_links (project_id, space_id, target_memory_id);

CREATE FUNCTION set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;

CREATE TRIGGER projects_set_updated_at
  BEFORE UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER memory_spaces_set_updated_at
  BEFORE UPDATE ON memory_spaces
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER memories_set_updated_at
  BEFORE UPDATE ON memories
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
ALTER TABLE memory_spaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_spaces FORCE ROW LEVEL SECURITY;
ALTER TABLE memories ENABLE ROW LEVEL SECURITY;
ALTER TABLE memories FORCE ROW LEVEL SECURITY;
ALTER TABLE memory_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_links FORCE ROW LEVEL SECURITY;

CREATE POLICY projects_isolation_policy ON projects
  USING (id = NULLIF(current_setting('app.current_project_id', true), '')::uuid)
  WITH CHECK (id = NULLIF(current_setting('app.current_project_id', true), '')::uuid);

CREATE POLICY memory_spaces_isolation_policy ON memory_spaces
  USING (project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid)
  WITH CHECK (project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid);

CREATE POLICY memories_isolation_policy ON memories
  USING (project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid)
  WITH CHECK (project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid);

CREATE POLICY memory_links_isolation_policy ON memory_links
  USING (project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid)
  WITH CHECK (project_id = NULLIF(current_setting('app.current_project_id', true), '')::uuid);

