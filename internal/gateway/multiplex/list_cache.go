package multiplex

import (
	"encoding/json"
	"sync"
	"time"
)

type listCache struct {
	mu       sync.RWMutex
	payload  json.RawMessage
	storedAt time.Time
	ttl      time.Duration
}

func (c *listCache) load() (json.RawMessage, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ttl <= 0 || len(c.payload) == 0 || time.Since(c.storedAt) >= c.ttl {
		return nil, false
	}
	return append(json.RawMessage(nil), c.payload...), true
}

func (c *listCache) store(payload json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl <= 0 {
		return
	}
	c.payload = append(json.RawMessage(nil), payload...)
	c.storedAt = time.Now()
}

func (c *listCache) invalidate() {
	c.mu.Lock()
	c.payload = nil
	c.mu.Unlock()
}
