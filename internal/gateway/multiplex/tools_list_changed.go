package multiplex

import (
	"context"
	"encoding/json"
	"log/slog"
)

func (a *Multiplexer) HandleToolsListChanged() {
	a.invalidateListCache()
	if a.semantic == nil || !a.semantic.Enabled() {
		return
	}
	a.listChangedDebouncer.schedule(a.runToolsListChangedRefresh)
}

func (a *Multiplexer) runToolsListChangedRefresh() {
	tctx, cancel := context.WithTimeout(a.lifecycleCtx, a.listTimeout)
	defer cancel()

	merged, failures, err := a.fetchAndMergeUpstreamTools(tctx)
	if err != nil {
		slog.Warn("tools/list_changed refresh skipped", "err", err)
		return
	}
	if len(a.upstreams) > 0 && len(failures) == len(a.upstreams) {
		slog.Warn("tools/list_changed refresh skipped",
			"reason", "every upstream failed",
			"upstreams", len(a.upstreams))
		return
	}
	a.replaceToolSchemasFromMerged(merged)

	outFull, err := json.Marshal(map[string]any{"tools": merged})
	if err != nil {
		slog.Warn("tools/list_changed refresh marshal failed", "err", err)
		return
	}
	a.maybeReindexSemanticCatalog(tctx, merged, outFull)
}
