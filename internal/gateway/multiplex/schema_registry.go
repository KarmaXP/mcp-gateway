package multiplex

import "sync"

type schemaRegistry struct {
	mu         sync.RWMutex
	byToolName map[string]toolSchema
}

func (r *schemaRegistry) lookup(namespacedTool string) toolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byToolName[namespacedTool]
}

func (r *schemaRegistry) replace(byToolName map[string]toolSchema) {
	r.mu.Lock()
	r.byToolName = byToolName
	r.mu.Unlock()
}
