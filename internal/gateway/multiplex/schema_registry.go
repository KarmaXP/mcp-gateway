package multiplex

import "sync"

type schemaRegistry struct {
	mu         sync.RWMutex
	byToolName map[string]toolSchema
	byUpstream map[string]map[string]toolSchema
}

func (r *schemaRegistry) lookup(namespacedTool string) toolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byToolName[namespacedTool]
}

func (r *schemaRegistry) replaceReachable(answered map[string]map[string]toolSchema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byUpstream == nil {
		r.byUpstream = make(map[string]map[string]toolSchema, len(answered))
	}
	for upstreamID, tools := range answered {
		r.byUpstream[upstreamID] = tools
	}
	flat := make(map[string]toolSchema)
	for _, tools := range r.byUpstream {
		for name, schema := range tools {
			flat[name] = schema
		}
	}
	r.byToolName = flat
}
