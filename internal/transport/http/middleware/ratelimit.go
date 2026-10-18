package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"

	"github.com/Tomelin/extend-memory-ai/internal/auth"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/api"
	"github.com/Tomelin/extend-memory-ai/internal/transport/http/problem"
)

// idleLimiterTTL is how long a key's bucket is kept without requests.
const idleLimiterTTL = 10 * time.Minute

// RateLimit applies a token bucket per API key (rps sustained, burst peak).
// Buckets live in this process: with N replicas the effective limit is N×rps.
// rps <= 0 disables the middleware. It must run after Authenticate.
func RateLimit(meter metric.Meter, rps float64, burst int) (gin.HandlerFunc, error) {
	if rps <= 0 {
		return func(c *gin.Context) { c.Next() }, nil
	}
	rejected, err := meter.Int64Counter("ratelimit.rejected",
		metric.WithDescription("Requests rejected by the per-key rate limit."), metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	l := &keyLimiters{rps: rate.Limit(rps), burst: max(burst, 1), buckets: map[uuid.UUID]*bucket{}, now: time.Now}
	return func(c *gin.Context) {
		p, ok := auth.PrincipalFromContext(c.Request.Context())
		if !ok {
			c.Next()
			return
		}
		if wait := l.reserve(p.KeyID); wait > 0 {
			rejected.Add(c.Request.Context(), 1)
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			problem.Abort(c, http.StatusTooManyRequests, api.ProblemCodeRateLimited, "limite de requisições da chave excedido")
			return
		}
		c.Next()
	}, nil
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type keyLimiters struct {
	mu        sync.Mutex
	rps       rate.Limit
	burst     int
	buckets   map[uuid.UUID]*bucket
	lastSweep time.Time
	now       func() time.Time
}

// reserve takes a token for key and returns 0, or how long the client must
// wait when the bucket is empty (the token is then given back).
func (k *keyLimiters) reserve(key uuid.UUID) time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if now.Sub(k.lastSweep) > idleLimiterTTL {
		for id, b := range k.buckets {
			if now.Sub(b.lastSeen) > idleLimiterTTL {
				delete(k.buckets, id)
			}
		}
		k.lastSweep = now
	}
	b, ok := k.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(k.rps, k.burst)}
		k.buckets[key] = b
	}
	b.lastSeen = now
	r := b.limiter.ReserveN(now, 1)
	if wait := r.DelayFrom(now); wait > 0 {
		r.CancelAt(now)
		return wait
	}
	return 0
}
