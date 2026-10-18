-- Run with psql as the production DBA/superuser after roles and databases have
-- been provisioned. Keep the role locked instead of dropping it because it can
-- be recorded as the grantor of privileges created by 002-roles.sql.

\set ON_ERROR_STOP on

BEGIN;

ALTER ROLE memory_provisioner
    NOLOGIN
    NOCREATEDB
    NOCREATEROLE
    NOBYPASSRLS
    PASSWORD NULL
    VALID UNTIL '1970-01-01 00:00:00+00';

REVOKE CONNECT ON DATABASE :"DBNAME" FROM memory_provisioner;

COMMENT ON ROLE memory_provisioner IS
    'Disabled production provisioner for Extend Memory AI';

COMMIT;

