package postgres

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/Tomelin/extend-memory-ai/database"
)

// NewMigrator returns a golang-migrate instance over the embedded migrations.
// databaseURL may use the postgres:// or postgresql:// scheme.
func NewMigrator(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(database.Migrations, "migrations")
	if err != nil {
		return nil, err
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("open migrator: %w", err)
	}
	return m, nil
}

// LatestVersion returns the highest embedded migration version, which is the
// schema version the binary expects.
func LatestVersion() (uint, error) {
	src, err := iofs.New(database.Migrations, "migrations")
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()
	v, err := src.First()
	if err != nil {
		return 0, err
	}
	for {
		next, err := src.Next(v)
		if errors.Is(err, fs.ErrNotExist) {
			return v, nil
		}
		if err != nil {
			return 0, err
		}
		v = next
	}
}

func migrateURL(databaseURL string) string {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if rest, ok := strings.CutPrefix(databaseURL, scheme); ok {
			return "pgx5://" + rest
		}
	}
	return databaseURL
}
