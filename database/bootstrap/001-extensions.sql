-- Cluster-level objects. Run once per environment by a superuser or DBA,
-- before any migration.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
