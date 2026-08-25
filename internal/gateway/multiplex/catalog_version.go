package multiplex

import (
	"sync"
	"sync/atomic"
)

type catalogVersion struct {
	mu         sync.RWMutex
	version    string
	refreshGen atomic.Uint64
}

func (c *catalogVersion) load() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

func (c *catalogVersion) isCurrent(ver string) bool {
	return c.load() == ver
}

func (c *catalogVersion) beginRefresh() uint64 {
	return c.refreshGen.Add(1)
}

func (c *catalogVersion) commitIfCurrent(ver string, generation uint64, apply func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refreshGen.Load() != generation || c.version == ver {
		return
	}
	apply()
	c.version = ver
}
