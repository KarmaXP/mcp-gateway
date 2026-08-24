package multiplex

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

func (a *Multiplexer) HandleToolsListChanged(ctx context.Context) {
	a.invalidateToolCache()
	if a.semantic == nil || !a.semantic.Enabled() {
		return
	}
	a.scheduleToolsListChangedRefresh(ctx)
}

func (a *Multiplexer) scheduleToolsListChangedRefresh(trigger context.Context) {
	delay := a.listChangedDebounce
	if delay <= 0 {
		a.runToolsListChangedRefresh(trigger)
		return
	}

	a.listChangedMu.Lock()
	defer a.listChangedMu.Unlock()

	if a.listChangedTimer != nil {
		a.listChangedTimer.Stop()
	}
	a.listChangedGeneration++
	generation := a.listChangedGeneration
	a.listChangedPendingCtx = trigger
	a.listChangedTimer = time.AfterFunc(delay, func() {
		a.listChangedMu.Lock()
		if generation != a.listChangedGeneration {
			a.listChangedMu.Unlock()
			return
		}
		pending := a.listChangedPendingCtx
		a.listChangedPendingCtx = nil
		a.listChangedTimer = nil
		a.listChangedMu.Unlock()
		a.runToolsListChangedRefresh(pending)
	})
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
