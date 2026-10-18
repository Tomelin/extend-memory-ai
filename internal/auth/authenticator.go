package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

const instrumentationName = "github.com/Tomelin/extend-memory-ai/internal/auth"

// Failure reasons recorded in the auth.failures metric.
const (
	ReasonMissing = "missing"
	ReasonInvalid = "invalid"
	ReasonScope   = "scope"
)

// ErrUnauthenticated is returned for any missing, malformed, unknown, revoked
// or expired key. Callers must not reveal which case happened.
var ErrUnauthenticated = errors.New("unauthenticated")

// CredentialStore resolves keys by prefix. ResolveAPIKey returns
// domain.ErrNotFound for unknown, revoked or expired keys.
type CredentialStore interface {
	ResolveAPIKey(ctx context.Context, prefix string) (domain.APIKeyCredential, error)
	TouchAPIKey(ctx context.Context, keyID uuid.UUID) error
}

// Options configures the Authenticator.
type Options struct {
	CacheTTL         time.Duration
	NegativeCacheTTL time.Duration
	MaxCacheEntries  int
	Logger           *slog.Logger
	Now              func() time.Time
}

// Authenticator verifies API keys, caching resolved keys by prefix.
type Authenticator struct {
	store    CredentialStore
	cache    *keyCache
	opts     Options
	tracer   trace.Tracer
	failures metric.Int64Counter
	lookups  metric.Int64Counter
	touchSem chan struct{}
}

// NewAuthenticator creates an Authenticator using the global OpenTelemetry
// providers.
func NewAuthenticator(store CredentialStore, opts Options) (*Authenticator, error) {
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = time.Minute
	}
	if opts.NegativeCacheTTL <= 0 {
		opts.NegativeCacheTTL = 5 * time.Second
	}
	if opts.MaxCacheEntries <= 0 {
		opts.MaxCacheEntries = 10_000
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	meter := otel.Meter(instrumentationName)
	failures, err := meter.Int64Counter("auth.failures",
		metric.WithDescription("Rejected authentication or authorization attempts."),
		metric.WithUnit("{failure}"))
	if err != nil {
		return nil, err
	}
	lookups, err := meter.Int64Counter("auth.key_cache.lookups",
		metric.WithDescription("API key cache lookups."),
		metric.WithUnit("{lookup}"))
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		store:    store,
		cache:    newKeyCache(opts.MaxCacheEntries, opts.Now),
		opts:     opts,
		tracer:   otel.Tracer(instrumentationName),
		failures: failures,
		lookups:  lookups,
		touchSem: make(chan struct{}, 4),
	}, nil
}

// Authenticate verifies the token and returns the principal. It returns
// ErrUnauthenticated for any invalid credential and another error when the
// store is unavailable.
func (a *Authenticator) Authenticate(ctx context.Context, token string) (domain.Principal, error) {
	ctx, span := a.tracer.Start(ctx, "auth.resolve_key")
	defer span.End()

	prefix, secret, err := ParseToken(token)
	if err != nil {
		a.RecordFailure(ctx, ReasonInvalid)
		return domain.Principal{}, ErrUnauthenticated
	}

	cred, found, hit := a.cache.get(prefix)
	span.SetAttributes(attribute.Bool("cache.hit", hit))
	a.lookups.Add(ctx, 1, metric.WithAttributes(attribute.String("result", hitLabel(hit))))
	if !hit {
		cred, err = a.store.ResolveAPIKey(ctx, prefix)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			a.cache.put(prefix, domain.APIKeyCredential{}, false, a.opts.NegativeCacheTTL)
		case err != nil:
			span.RecordError(err)
			span.SetStatus(codes.Error, "resolve api key")
			return domain.Principal{}, err
		default:
			found = true
			a.cache.put(prefix, cred, true, a.opts.CacheTTL)
			a.touch(cred.KeyID)
		}
	}

	if !found || !VerifySecret(secret, cred.SecretHash) {
		a.RecordFailure(ctx, ReasonInvalid)
		return domain.Principal{}, ErrUnauthenticated
	}
	return domain.Principal{ProjectID: cred.ProjectID, KeyID: cred.KeyID, Scopes: cred.Scopes}, nil
}

// RecordFailure increments auth.failures with the given reason.
func (a *Authenticator) RecordFailure(ctx context.Context, reason string) {
	a.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// touch updates last_used_at asynchronously. It runs at most once per cache
// miss of a valid key (so roughly once per TTL per key) and is dropped when
// too many updates are already in flight.
func (a *Authenticator) touch(keyID uuid.UUID) {
	select {
	case a.touchSem <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-a.touchSem }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := a.store.TouchAPIKey(ctx, keyID); err != nil {
			a.opts.Logger.WarnContext(ctx, "update api key last_used_at", "key_id", keyID.String(), "error", err)
		}
	}()
}

func hitLabel(hit bool) string {
	if hit {
		return "hit"
	}
	return "miss"
}
