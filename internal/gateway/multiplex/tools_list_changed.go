package multiplex

import (
	"context"
	"encoding/json"
	"log/slog"
)

func (a *Multiplexer) HandleToolsListChanged(ctx context.Context) {
	a.invalidateToolCache()
	if a.semantic == nil || !a.semantic.Enabled() {
		return
	}
	a.listChangedDebouncer.schedule(func() { a.runToolsListChangedRefresh(ctx) })
}

func (a *Multiplexer) runToolsListChangedRefresh(trigger context.Context) {
	ctx := a.lifecycleContext(trigger)
	tctx, cancel := context.WithTimeout(ctx, a.listTimeout)
	defer cancel()

	merged, _, err := a.fetchAndMergeUpstreamTools(tctx)
	if err != nil {
		slog.Warn("tools/list_changed refresh skipped", "err", err)
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
