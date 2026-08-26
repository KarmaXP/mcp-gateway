package multiplex

import (
	"encoding/json"
	"sync"
)

type initializeOutcome struct {
	mu           sync.Mutex
	strict       bool
	strictFailed bool
	results      []json.RawMessage
	failures     []PartialFailure
}

func newInitializeOutcome(strict bool, upstreams int) *initializeOutcome {
	return &initializeOutcome{strict: strict, results: make([]json.RawMessage, upstreams)}
}

func (o *initializeOutcome) recordFailure(upstreamID, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.strict {
		o.strictFailed = true
		return
	}
	o.failures = append(o.failures, PartialFailure{UpstreamID: upstreamID, Reason: reason})
}

func (o *initializeOutcome) recordResult(index int, result json.RawMessage) {
	o.mu.Lock()
	o.results[index] = append(json.RawMessage(nil), result...)
	o.mu.Unlock()
}
