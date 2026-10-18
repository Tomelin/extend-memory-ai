// Package database embeds the SQL assets so binaries and tests do not depend
// on the working directory.
package database

import "embed"

// Migrations holds the golang-migrate files (schema, indexes, RLS and grants).
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Bootstrap holds the cluster-level scripts (extensions and roles) that a
// superuser runs once per environment, before any migration.
//
//go:embed bootstrap/*.sql
var Bootstrap embed.FS
