package multiplex

import (
	"context"
	"encoding/json"
	"log/slog"
)

// HandleToolsListChanged invalidates the tools cache and refreshes the semantic catalog index.
func (a *Multiplexer) HandleToolsListChanged(ctx context.Context) {
	a.invalidateToolCache()
	if a.semantic == nil || !a.semantic.Enabled() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tctx, cancel := context.WithTimeout(ctx, a.listTimeout)
	defer cancel()

	merged, _, err := a.fetchAndMergeUpstreamTools(tctx)
	if err != nil {
		slog.Warn("tools/list_changed refresh skipped", "err", err)
		return
	}
	outFull, err := json.Marshal(map[string]any{"tools": merged})
	if err != nil {
		slog.Warn("tools/list_changed refresh marshal failed", "err", err)
		return
	}
	a.maybeReindexSemanticCatalog(tctx, merged, outFull)
}
