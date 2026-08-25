package multiplex

import (
	"encoding/json"
	"sync"
)

type initializeOnce struct {
	mu     sync.Mutex
	done   bool
	result json.RawMessage
}

func (o *initializeOnce) load() (json.RawMessage, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.done {
		return nil, false
	}
	return append(json.RawMessage(nil), o.result...), true
}

func (o *initializeOnce) store(result json.RawMessage) {
	o.mu.Lock()
	o.done = true
	o.result = append(json.RawMessage(nil), result...)
	o.mu.Unlock()
}
