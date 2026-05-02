package policy

import "sync/atomic"

// Atomically reloadable policy.Engine (e.g. SIGHUP).
type Holder struct {
	p atomic.Pointer[Engine]
}

func NewHolder(e *Engine) *Holder {
	h := &Holder{}
	h.Store(e)
	return h
}

func (h *Holder) Load() *Engine {
	if h == nil {
		return nil
	}
	return h.p.Load()
}

func (h *Holder) Store(e *Engine) {
	if h == nil {
		return
	}
	h.p.Store(e)
}
