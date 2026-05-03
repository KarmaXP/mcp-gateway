package router

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// FilterListActive is true when router.mode is filter_list (intent-based tools/list subsetting).
func (sr *SemanticRouter) FilterListActive() bool {
	return sr != nil && sr.cfg.Mode == ModeFilterList
}

// FilterToolsForList ranks tools by intent and returns names to keep, or useFull=true to serve the merged catalog
// (after allow-list only). Degrades to full list on empty intent, stale catalog version, embed/query errors, or no hits.
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
