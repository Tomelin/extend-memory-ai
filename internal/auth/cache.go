package auth

import (
	"sync"
	"time"

	"github.com/Tomelin/extend-memory-ai/internal/domain"
)

type cacheEntry struct {
	cred    domain.APIKeyCredential
	found   bool
	expires time.Time
}

// keyCache caches resolved keys by prefix. Entries with found=false are
// negative entries for unknown prefixes.
type keyCache struct {
	mu         sync.Mutex
	entries    map[string]cacheEntry
	maxEntries int
	now        func() time.Time
}

func newKeyCache(maxEntries int, now func() time.Time) *keyCache {
	return &keyCache{entries: make(map[string]cacheEntry), maxEntries: maxEntries, now: now}
}

// get returns the cached credential, whether the key exists, and whether
// there was a valid cache entry.
func (c *keyCache) get(prefix string) (domain.APIKeyCredential, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[prefix]
	if !ok {
		return domain.APIKeyCredential{}, false, false
	}
	if !c.now().Before(e.expires) {
		delete(c.entries, prefix)
		return domain.APIKeyCredential{}, false, false
	}
	return e.cred, e.found, true
}

func (c *keyCache) put(prefix string, cred domain.APIKeyCredential, found bool, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, exists := c.entries[prefix]; !exists && len(c.entries) >= c.maxEntries {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.maxEntries {
			clear(c.entries)
		}
	}
	c.entries[prefix] = cacheEntry{cred: cred, found: found, expires: now.Add(ttl)}
}

func (c *keyCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
