package indexer

import (
	"sync"
	"time"
)

// cache holds recent result sets so repeating a query does not make the
// aggregator fan out to every tracker again.
//
// It earns its place in the TUI rather than the CLI, which is a fresh
// process each time: a fan-out query takes seconds, there is no spinner
// anywhere in the interface, and the commonest thing a user does is esc
// out of the results and search the same thing again a moment later.
//
// Guarded by a mutex because the TUI runs its searches on bubbletea's
// command goroutines, so two can be in flight at once.
type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	results []Result
	expires time.Time
}

// get returns a result set that has not expired.
func (c *cache) get(key string, now time.Time) ([]Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok || !now.Before(e.expires) {
		return nil, false
	}
	return e.results, true
}

// put stores a result set, doing nothing when the cache is switched off.
//
// The map is capped rather than left to grow: a long TUI session is the
// case this exists for, and an unbounded map keyed on user input is the
// kind of leak nobody notices until a daemon has been up for a week.
// Over the cap it drops the entry expiring soonest, which is the one
// with least left to give.
func (c *cache) put(key string, results []Result, expires time.Time, ttl time.Duration) {
	if ttl < 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[string]cacheEntry)
	}
	if _, replacing := c.entries[key]; !replacing && len(c.entries) >= maxCacheEntries {
		var oldestKey string
		var oldest time.Time
		for k, e := range c.entries {
			if oldest.IsZero() || e.expires.Before(oldest) {
				oldestKey, oldest = k, e.expires
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = cacheEntry{results: results, expires: expires}
}
