package policy

import "sync/atomic"

// Holder holds the active policy Engine for atomic reload (SIGHUP / future config watchers).
type Holder struct {
	p atomic.Pointer[Engine]
}

// NewHolder returns a holder with an initial engine (may be nil).
func NewHolder(e *Engine) *Holder {
	h := &Holder{}
	h.Store(e)
	return h
}

// Load returns the current engine or nil.
func (h *Holder) Load() *Engine {
	if h == nil {
		return nil
	}
	return h.p.Load()
}

// Store swaps the active engine (callers typically discard the old value; GC collects it).
func (h *Holder) Store(e *Engine) {
	if h == nil {
		return
	}
	h.p.Store(e)
}
