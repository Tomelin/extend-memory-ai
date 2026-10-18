-- Run with psql, as the production DBA/superuser, connected to the database
-- where database/bootstrap/002-roles.sql will be executed.
--
-- Example:
--   psql "$DBA_DATABASE_URL" -X \
--     -f database/provisioning/create-memory-provisioner.sql
--
-- The script intentionally fails when memory_provisioner already exists. It
-- never changes an existing role whose origin cannot be established.

\set ON_ERROR_STOP on

BEGIN;

DO $provisioner$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_roles
        WHERE rolname = 'memory_provisioner'
    ) THEN
        RAISE EXCEPTION
            'role memory_provisioner already exists; inspect it before proceeding';
    END IF;
END
$provisioner$;

-- BYPASSRLS is required because 002-roles.sql creates memory_admin with that
-- attribute. CREATEDB permits creation of the additional application database.
CREATE ROLE memory_provisioner
    LOGIN
    NOSUPERUSER
    CREATEDB
    CREATEROLE
    NOREPLICATION
    BYPASSRLS
    CONNECTION LIMIT 1
    PASSWORD NULL;

-- Limit this temporary credential to one day even if the cleanup step is
-- accidentally omitted.
SELECT format(
    'ALTER ROLE memory_provisioner VALID UNTIL %L',
    current_timestamp + interval '24 hours'
)
\gexec

-- 002-roles.sql grants privileges on public, so the provisioner needs those
-- privileges with grant option in the database to which psql is connected.
GRANT CONNECT ON DATABASE :"DBNAME" TO memory_provisioner;
GRANT CREATE, USAGE ON SCHEMA public
    TO memory_provisioner WITH GRANT OPTION;

COMMENT ON ROLE memory_provisioner IS
    'Temporary role for provisioning Extend Memory AI; disable after use';

COMMIT;

\echo
\echo Set a one-time password for memory_provisioner.
\password memory_provisioner

