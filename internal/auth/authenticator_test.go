package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

type fakeStore struct {
	mu       sync.Mutex
	creds    map[string]domain.APIKeyCredential
	err      error
	resolves int
	touched  chan uuid.UUID
}

func (f *fakeStore) ResolveAPIKey(_ context.Context, prefix string) (domain.APIKeyCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolves++
	if f.err != nil {
		return domain.APIKeyCredential{}, f.err
	}
	c, ok := f.creds[prefix]
	if !ok {
		return domain.APIKeyCredential{}, domain.ErrNotFound
	}
	return c, nil
}

func (f *fakeStore) TouchAPIKey(_ context.Context, keyID uuid.UUID) error {
	f.touched <- keyID
	return nil
}

func (f *fakeStore) resolveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolves
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	store *fakeStore
	clock *clock
	authn *Authenticator
	key   GeneratedKey
	cred  domain.APIKeyCredential
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cred := domain.APIKeyCredential{
		KeyID:      uuid.New(),
		ProjectID:  uuid.New(),
		SecretHash: key.SecretHash,
		Scopes:     []domain.Scope{domain.ScopeMemoriesRead},
	}
	store := &fakeStore{creds: map[string]domain.APIKeyCredential{key.Prefix: cred}, touched: make(chan uuid.UUID, 10)}
	clk := &clock{now: time.Unix(1_700_000_000, 0)}
	authn, err := NewAuthenticator(store, Options{
		CacheTTL:         time.Minute,
		NegativeCacheTTL: 5 * time.Second,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:              clk.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: store, clock: clk, authn: authn, key: key, cred: cred}
}

func TestAuthenticateValidKeyIsCached(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	for range 3 {
		p, err := f.authn.Authenticate(ctx, f.key.Token)
		if err != nil {
			t.Fatalf("authenticate: %v", err)
		}
		if p.ProjectID != f.cred.ProjectID || p.KeyID != f.cred.KeyID || !p.HasScope(domain.ScopeMemoriesRead) {
			t.Fatalf("unexpected principal %+v", p)
		}
	}
	if n := f.store.resolveCount(); n != 1 {
		t.Errorf("store resolved %d times, want 1", n)
	}
	select {
	case id := <-f.store.touched:
		if id != f.cred.KeyID {
			t.Errorf("touched %s, want %s", id, f.cred.KeyID)
		}
	case <-time.After(time.Second):
		t.Error("last_used_at was not updated")
	}
}

func TestAuthenticateWrongSecret(t *testing.T) {
	f := newFixture(t)
	other, _ := GenerateKey()
	forged := "emk_" + f.key.Prefix + other.Token[len("emk_")+len(other.Prefix):]
	if _, err := f.authn.Authenticate(context.Background(), forged); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestAuthenticateMalformedDoesNotHitStore(t *testing.T) {
	f := newFixture(t)
	if _, err := f.authn.Authenticate(context.Background(), "not-a-key"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	if n := f.store.resolveCount(); n != 0 {
		t.Errorf("store resolved %d times, want 0", n)
	}
}

func TestAuthenticateUnknownPrefixIsNegativelyCached(t *testing.T) {
	f := newFixture(t)
	unknown, _ := GenerateKey()
	ctx := context.Background()

	for range 2 {
		if _, err := f.authn.Authenticate(ctx, unknown.Token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("err = %v, want ErrUnauthenticated", err)
		}
	}
	if n := f.store.resolveCount(); n != 1 {
		t.Fatalf("store resolved %d times, want 1", n)
	}
	f.clock.Advance(6 * time.Second)
	_, _ = f.authn.Authenticate(ctx, unknown.Token)
	if n := f.store.resolveCount(); n != 2 {
		t.Errorf("store resolved %d times after negative TTL, want 2", n)
	}
}

func TestRevokedKeyStopsWorkingAfterTTL(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.authn.Authenticate(ctx, f.key.Token); err != nil {
		t.Fatal(err)
	}

	f.store.mu.Lock()
	delete(f.store.creds, f.key.Prefix)
	f.store.mu.Unlock()

	if _, err := f.authn.Authenticate(ctx, f.key.Token); err != nil {
		t.Fatalf("within TTL: %v", err)
	}
	f.clock.Advance(time.Minute)
	if _, err := f.authn.Authenticate(ctx, f.key.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after TTL: err = %v, want ErrUnauthenticated", err)
	}
}

func TestStoreErrorIsNotCachedNorUnauthenticated(t *testing.T) {
	f := newFixture(t)
	f.store.err = domain.ErrUnavailable
	ctx := context.Background()
	for range 2 {
		_, err := f.authn.Authenticate(ctx, f.key.Token)
		if !errors.Is(err, domain.ErrUnavailable) || errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	}
	if n := f.store.resolveCount(); n != 2 {
		t.Errorf("store resolved %d times, want 2", n)
	}
}

func TestKeyCacheBoundsEntries(t *testing.T) {
	clk := &clock{now: time.Unix(0, 0)}
	c := newKeyCache(2, clk.Now)
	c.put("a", domain.APIKeyCredential{}, true, time.Second)
	c.put("b", domain.APIKeyCredential{}, true, time.Minute)
	clk.Advance(2 * time.Second)
	c.put("c", domain.APIKeyCredential{}, true, time.Minute)
	if _, _, ok := c.get("a"); ok {
		t.Error("expired entry a still cached")
	}
	if _, _, ok := c.get("b"); !ok {
		t.Error("entry b evicted although cache had room after purging expired entries")
	}
	c.put("d", domain.APIKeyCredential{}, false, time.Minute)
	if len(c.entries) > 2 {
		t.Errorf("cache has %d entries, max 2", len(c.entries))
	}
	if _, found, ok := c.get("d"); !ok || found {
		t.Errorf("negative entry d: ok=%v found=%v", ok, found)
	}
}
