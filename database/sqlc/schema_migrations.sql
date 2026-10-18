-- Table managed by golang-migrate. Declared here only so sqlc can type the
-- queries that read it; it is never applied to the database.
CREATE TABLE schema_migrations (
  version bigint NOT NULL PRIMARY KEY,
  dirty boolean NOT NULL
);
