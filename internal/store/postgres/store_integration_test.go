//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/domain"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres"
	"github.com/Tomelin/extend-memory-ai/internal/store/postgres/sqlcdb"
	"github.com/Tomelin/extend-memory-ai/internal/testsupport/pgtest"
)

var db *pgtest.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	db, err = pgtest.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := db.MigrateUp(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = db.Terminate(ctx)
		os.Exit(1)
	}
	code := m.Run()
	_ = db.Terminate(ctx)
	os.Exit(code)
}

func pool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func newProject(t *testing.T, admin *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	slug := "p-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err := admin.QueryRow(context.Background(),
		"INSERT INTO projects (slug, name) VALUES ($1, $2) RETURNING id", slug, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestResolveAPIKey(t *testing.T) {
	ctx := context.Background()
	admin := pool(t, db.AdminURL)
	store := postgres.New(pool(t, db.AppURL))
	projectID := newProject(t, admin)
	q := sqlcdb.New(admin)

	insert := func(expiresAt *time.Time) auth.GeneratedKey {
		k, err := auth.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.InsertAPIKey(ctx, sqlcdb.InsertAPIKeyParams{
			ProjectID: projectID, Prefix: k.Prefix, SecretHash: k.SecretHash,
			Name: "test", Scopes: []string{"memories:read"}, ExpiresAt: expiresAt,
		}); err != nil {
			t.Fatal(err)
		}
		return k
	}

	active := insert(nil)
	cred, err := store.ResolveAPIKey(ctx, active.Prefix)
	if err != nil {
		t.Fatalf("active key: %v", err)
	}
	if cred.ProjectID != projectID || string(cred.SecretHash) != string(active.SecretHash) ||
		len(cred.Scopes) != 1 || cred.Scopes[0] != domain.ScopeMemoriesRead {
		t.Errorf("unexpected credential %+v", cred)
	}
	if err := store.TouchAPIKey(ctx, cred.KeyID); err != nil {
		t.Errorf("touch: %v", err)
	}

	revoked := insert(nil)
	if _, err := q.RevokeAPIKey(ctx, revoked.Prefix); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	expired := insert(&past)

	for name, prefix := range map[string]string{"revoked": revoked.Prefix, "expired": expired.Prefix, "unknown": "aaaaaaaaaaaaa"} {
		if _, err := store.ResolveAPIKey(ctx, prefix); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s key: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestWithProjectScopesRLSAndRollsBack(t *testing.T) {
	ctx := context.Background()
	admin := pool(t, db.AdminURL)
	store := postgres.New(pool(t, db.AppURL))
	a, b := newProject(t, admin), newProject(t, admin)

	p, err := store.GetProject(ctx, a)
	if err != nil || p.ID != a {
		t.Fatalf("own project: %+v %v", p, err)
	}
	err = store.WithProject(ctx, a, func(q *sqlcdb.Queries) error {
		_, err := q.GetProject(ctx, b)
		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other project visible: err = %v", err)
	}

	sentinel := errors.New("abort")
	if err := store.WithProject(ctx, a, func(*sqlcdb.Queries) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("fn error not returned: %v", err)
	}
}

func TestCheckSchema(t *testing.T) {
	ctx := context.Background()
	store := postgres.New(pool(t, db.AppURL))
	latest, err := postgres.LatestVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CheckSchema(ctx, latest, 1536); err != nil {
		t.Fatalf("current schema rejected: %v", err)
	}
	err = store.CheckSchema(ctx, latest+1, 1536)
	if !errors.Is(err, postgres.ErrSchemaMismatch) || !strings.Contains(err.Error(), fmt.Sprintf("found version %d, expected %d", latest, latest+1)) {
		t.Errorf("version mismatch: %v", err)
	}
	if err := store.CheckSchema(ctx, latest, 768); !errors.Is(err, postgres.ErrSchemaMismatch) {
		t.Errorf("dimension mismatch: %v", err)
	}
}
