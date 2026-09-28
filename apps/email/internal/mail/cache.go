package mail

import (
	"sync"
	"time"
)

type cacheKey struct {
	account int64
	folder  string
	query   string
	page    int
}

type cacheEntry struct {
	at   time.Time
	page Page
}

type listCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[cacheKey]cacheEntry
}

func newListCache(ttl time.Duration) *listCache {
	return &listCache{ttl: ttl, m: map[cacheKey]cacheEntry{}}
}

func (c *listCache) get(account int64, folder, query string, page int) (Page, bool) {
	if c == nil {
		return Page{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[cacheKey{account, folder, query, page}]
	if !ok || time.Since(e.at) > c.ttl {
		return Page{}, false
	}
	return e.page, true
}

func (c *listCache) put(account int64, folder, query string, page int, p Page) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[cacheKey{account, folder, query, page}] = cacheEntry{at: time.Now(), page: p}
}

func (c *listCache) drop(account int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if k.account == account {
			delete(c.m, k)
		}
	}
}
