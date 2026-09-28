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
	key := cacheKey{account, folder, query, page}
	e, ok := c.m[key]
	if !ok {
		return Page{}, false
	}
	if time.Since(e.at) > c.ttl {
		delete(c.m, key)
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
	now := time.Now()
	for k, e := range c.m {
		if now.Sub(e.at) > c.ttl {
			delete(c.m, k)
		}
	}
	key := cacheKey{account, folder, query, page}
	if _, ok := c.m[key]; !ok && len(c.m) >= maxCacheEntries {
		var oldest cacheKey
		var at time.Time
		first := true
		for k, e := range c.m {
			if first || e.at.Before(at) {
				oldest, at, first = k, e.at, false
			}
		}
		if !first {
			delete(c.m, oldest)
		}
	}
	c.m[key] = cacheEntry{at: now, page: p}
}

// markSeen updates \Seen on cached header rows for one UID. The next list
// page can stay on the header cache instead of refetching after open or
// mark-unread.
func (c *listCache) markSeen(account int64, folder string, uid uint32, seen bool) {
	if c == nil || account == 0 || uid == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.m {
		if k.account != account || k.folder != folder {
			continue
		}
		changed := false
		for i := range e.page.Messages {
			if e.page.Messages[i].UID == uid && e.page.Messages[i].Seen != seen {
				e.page.Messages[i].Seen = seen
				changed = true
			}
		}
		if changed {
			c.m[k] = e
		}
	}
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
