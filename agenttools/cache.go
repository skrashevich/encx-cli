package agenttools

import (
	"encoding/json"
	"sync"
	"time"
)

// readCache memoizes read-tool results.
//
// A single agent turn typically asks for the same facts several times — the
// model reads the game state, then the level, then the state again to check
// itself. Each of those is a round trip to the engine, paced and slow, and the
// answers are identical.
//
// Only read tools are cached, and any mutating call clears the whole cache: once
// a code is submitted the level, sectors and action log are all suspect. The
// session also clears it at the start of every turn, so nothing survives from
// one player question to the next — a cached level would be worse than no
// assistant at all when the game has moved on.
type readCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
	now     func() time.Time
}

type cacheEntry struct {
	value    any
	storedAt time.Time
}

func newReadCache(ttl time.Duration) *readCache {
	if ttl <= 0 {
		return nil
	}
	return &readCache{
		ttl:     ttl,
		entries: map[string]cacheEntry{},
		now:     time.Now,
	}
}

// key identifies one call. Arguments are part of it: enc_level for level 3 and
// level 4 are different reads.
func cacheKey(tool string, args map[string]any) (string, bool) {
	if len(args) == 0 {
		return tool, true
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	return tool + "|" + string(encoded), true
}

func (c *readCache) get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.now().Sub(entry.storedAt) > c.ttl {
		delete(c.entries, key)
		return nil, false
	}
	return entry.value, true
}

func (c *readCache) put(key string, value any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{value: value, storedAt: c.now()}
}

func (c *readCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}
