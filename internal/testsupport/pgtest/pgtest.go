//go:build integration

// Package pgtest starts a disposable PostgreSQL (pgvector) container with the
// cluster bootstrap applied, for integration tests.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Tomelin/extend-memory-ai/database"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres"
)

const image = "pgvector/pgvector:pg17"

// DB is a running database with one connection URL per role.
type DB struct {
	container   *tcpostgres.PostgresContainer
	SuperURL    string
	MigratorURL string
	AppURL      string
	AdminURL    string
}

// Start runs the container and applies database/bootstrap. Every role gets a
// random password generated for this run.
func Start(ctx context.Context) (*DB, error) {
	password := randomPassword()
	c, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("extend_memory_for_ai"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword(password),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	db := &DB{container: c}
	db.SuperURL, err = c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, errors.Join(err, db.Terminate(ctx))
	}
	if err := db.bootstrap(ctx, password); err != nil {
		return nil, errors.Join(err, db.Terminate(ctx))
	}
	db.MigratorURL = withUser(db.SuperURL, "memory_migrator", password)
	db.AppURL = withUser(db.SuperURL, "memory_app", password)
	db.AdminURL = withUser(db.SuperURL, "memory_admin", password)
	return db, nil
}

func (db *DB) bootstrap(ctx context.Context, password string) error {
	conn, err := pgx.Connect(ctx, db.SuperURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	files, err := fs.Glob(database.Bootstrap, "bootstrap/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		sql, err := fs.ReadFile(database.Bootstrap, f)
		if err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	for _, role := range []string{"memory_migrator", "memory_app", "memory_admin"} {
		if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", role, password)); err != nil {
			return err
		}
	}
	return nil
}

// MigrateUp applies all embedded migrations.
func (db *DB) MigrateUp() error {
	m, err := postgres.NewMigrator(db.MigratorURL)
	if err != nil {
		return err
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// Terminate stops the container.
func (db *DB) Terminate(ctx context.Context) error {
	return db.container.Terminate(ctx)
}

func withUser(raw, role, password string) string {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	u.User = url.UserPassword(role, password)
	return u.String()
}

func randomPassword() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
