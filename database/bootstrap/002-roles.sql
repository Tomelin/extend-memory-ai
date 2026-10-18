-- Roles are cluster-level objects. Passwords are set by the infrastructure
-- (secret manager); see database/local for development credentials.
CREATE ROLE memory_migrator LOGIN;
CREATE ROLE memory_app LOGIN NOBYPASSRLS;
CREATE ROLE memory_admin LOGIN BYPASSRLS;

-- Since PostgreSQL 15, only the database owner can create objects in public.
GRANT CREATE, USAGE ON SCHEMA public TO memory_migrator;
GRANT USAGE ON SCHEMA public TO memory_app, memory_admin;
