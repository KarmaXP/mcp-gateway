package router

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// FilterListActive reports whether tools/list should apply intent-based filtering (ROUTER_MODE=filter_list).
func (sr *SemanticRouter) FilterListActive() bool {
	return sr != nil && sr.cfg.Mode == ModeFilterList
}

// FilterToolsForList returns namespaced tool names to keep in tools/list when FilterListActive and intent is set.
//
// Empty intent (no X-MCP-Intent on the RPC): returns useFull=true — the gateway serves the same merged catalog as
// assist_list after JWT/RAR allow-list filtering only (no vector subset). We do not reuse intent from earlier
// requests; each tools/list is scoped to the current request context.
//
// Embed/vector failures, stale catalog vs sig.CatalogVersion, empty vector results, or no indexed catalog yet:
// degrades to useFull=true so hosts still receive a usable list. AllowedTools are always enforced in the vector
// store filter (plan §3.B S1); callers must still apply JWT/RAR allow-list to the merged tools.
func (sr *SemanticRouter) FilterToolsForList(ctx context.Context, sig RoutingSignal) (keepNames map[string]struct{}, useFull bool) {
	if sr == nil || !sr.FilterListActive() {
		return nil, true
	}
	if strings.TrimSpace(sig.IntentText) == "" {
		return nil, true
	}
	if sig.CatalogVersion != "" && sr.CatalogVersion() != "" && sig.CatalogVersion != sr.CatalogVersion() {
		slog.WarnContext(ctx, "filter_list stale catalog, returning full list", "client", sig.CatalogVersion, "server", sr.CatalogVersion())
		return nil, true
	}
	if sr.CatalogVersion() == "" {
		slog.WarnContext(ctx, "filter_list index not ready, returning full list")
		return nil, true
	}

	_, _, filter, _ := sr.routingAllowanceAndAlias(sig)
	qtext := index.FormatQuery("", sig.IntentText, nil)

	embCtx, cancel := context.WithTimeout(ctx, sr.cfg.EmbedTimeout)
	vecs, err := sr.embedder.Embed(embCtx, []string{qtext})
	cancel()
	if err != nil {
		slog.WarnContext(ctx, "filter_list embed failed, returning full list", "err", err)
		return nil, true
	}
	if len(vecs) != singleQueryEmbeddingCount || len(vecs[0]) != sr.dim {
		slog.WarnContext(ctx, "filter_list invalid embedding, returning full list")
		return nil, true
	}
	qv := vecs[0]
	store.L2Normalize(qv)

	dec := &RoutingDecision{}
	results, err := sr.runVectorQuery(ctx, dec, time.Now(), qv, filter)
	if err != nil {
		slog.WarnContext(ctx, "filter_list vector query failed, returning full list", "err", err)
		return nil, true
	}
	results = sr.maybeHybridRerank(qtext, results)

	keepNames = make(map[string]struct{})
	for _, h := range results {
		if h.Score < sr.cfg.ScoreMin {
			continue
		}
		keepNames[h.ToolName] = struct{}{}
	}
	if len(keepNames) == 0 {
		slog.WarnContext(ctx, "filter_list no tools above threshold, returning full list")
		return nil, true
	}
	return keepNames, false
}
