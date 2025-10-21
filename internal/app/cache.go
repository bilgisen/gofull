// FILE: internal/app/cache.go
package app

import (
	"sync"
	"time"
)

// Cache is a simple in-memory TTL cache for article contents.
type Cache struct {
	mu    sync.RWMutex
	items map[string]CachedEntry
	ttl   time.Duration
}

// CachedEntry stores value and timestamp.
type CachedEntry struct {
	Value     string
	Timestamp time.Time
}

// NewCache creates a new Cache.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		items: make(map[string]CachedEntry),
		tl:    ttl,
	}
}

// Get returns value and true if present and fresh.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Since(entry.Timestamp) > c.ttl {
		// stale
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return "", false
	}
	return entry.Value, true
}

// Set inserts or updates key.
func (c *Cache) Set(key string, value string) {
	c.mu.Lock()
	c.items[key] = CachedEntry{Value: value, Timestamp: time.Now()}
	c.mu.Unlock()
}

// Size returns current number of items.
func (c *Cache) Size() int {
	c.mu.RLock()
	sz := len(c.items)
	c.mu.RUnlock()
	return sz
}

// Cleanup removes stale entries.
func (c *Cache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.items {
		if now.Sub(e.Timestamp) > c.ttl {
			delete(c.items, k)
		}
	}
}
