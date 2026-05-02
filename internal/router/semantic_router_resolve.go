package router

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func (sr *SemanticRouter) ResolveToolsCall(ctx context.Context, sig RoutingSignal) (string, *RoutingDecision, error) {
	if sr == nil || !sr.Enabled() {
		return sig.ToolName, &RoutingDecision{FallbackLayer: "none", Outcome: OutcomeNone}, nil
	}
	start := time.Now()
	dec := &RoutingDecision{FallbackLayer: "vector"}

	if decStale, err := sr.staleCatalogDecision(sig, start); err != nil {
		return "", decStale, err
	}

	allowed, toolForExact, filter, rl := sr.routingAllowanceAndAlias(sig)

	if name, ok := sr.tryExactToolResolution(ctx, sig, start, dec, rl, toolForExact, allowed); ok {
		return name, dec, nil
	}

	return sr.resolveByVectorSearch(ctx, sig, start, dec, allowed, toolForExact, filter)
}

func (sr *SemanticRouter) staleCatalogDecision(sig RoutingSignal, start time.Time) (*RoutingDecision, error) {
	if sig.CatalogVersion == "" || sr.CatalogVersion() == "" || sig.CatalogVersion == sr.CatalogVersion() {
		return nil, nil
	}
	dec := &RoutingDecision{
		FallbackLayer: "vector",
		Outcome:       OutcomeMissStaleCatalog,
		LatencyMS:     time.Since(start).Milliseconds(),
	}
	return dec, fmt.Errorf("%w: client %q vs server %q", ErrStaleCatalog, sig.CatalogVersion, sr.CatalogVersion())
}

func (sr *SemanticRouter) routingAllowanceAndAlias(sig RoutingSignal) (allowed []string, toolForExact string, filter store.VectorSearchFilter, rl *rules.Rules) {
	sr.mu.RLock()
	rl = sr.rules
	sr.mu.RUnlock()

	allowed = append([]string(nil), sig.AllowedTools...)
	if rl != nil {
		allowed = rl.NarrowAllowed(sig.IntentText, allowed, sr.listCatalog())
	}

	toolForExact = sig.ToolName
	if rl != nil {
		if c := rl.CanonicalAlias(sig.ToolName); c != "" {
			toolForExact = c
		}
	}

	filter = store.VectorSearchFilter{
		CatalogVersion:   sr.CatalogVersion(),
		AllowedToolNames: allowed,
	}
	return allowed, toolForExact, filter, rl
}

func (sr *SemanticRouter) tryExactToolResolution(ctx context.Context, sig RoutingSignal, start time.Time, dec *RoutingDecision, rl *rules.Rules, toolForExact string, allowed []string) (string, bool) {
	if toolForExact == "" || !sr.exactInCatalog(toolForExact) || !sr.allowed(toolForExact, allowed) {
		return "", false
	}

	dec.UpstreamID = sr.upstreamID(toolForExact)
	dec.ToolNameNamespaced = toolForExact
	dec.Confidence = exactMatchConfidence

	if rl != nil && strings.TrimSpace(sig.ToolName) != "" && toolForExact != sig.ToolName {
		dec.FallbackLayer = "rules"
		dec.Outcome = OutcomeRulesAlias
		dec.Candidates = []ScoredTool{{Name: toolForExact, Score: exactMatchConfidence, Source: "rules"}}
	} else {
		dec.FallbackLayer = "exact"
		dec.Outcome = OutcomeExact
		dec.Candidates = []ScoredTool{{Name: toolForExact, Score: exactMatchConfidence, Source: "exact"}}
	}
	dec.LatencyMS = time.Since(start).Milliseconds()
	slog.InfoContext(ctx, "router decision", "layer", dec.FallbackLayer, "tool", toolForExact, "latency_ms", dec.LatencyMS)
	return toolForExact, true
}

func (sr *SemanticRouter) resolveByVectorSearch(ctx context.Context, sig RoutingSignal, start time.Time, dec *RoutingDecision, allowed []string, toolForExact string, filter store.VectorSearchFilter) (string, *RoutingDecision, error) {
	qtext := index.FormatQuery(sig.ToolName, sig.IntentText, jsonKeys(sig.ArgumentsJSON))

	embCtx, cancel := context.WithTimeout(ctx, sr.cfg.EmbedTimeout)
	vecs, err := sr.embedder.Embed(embCtx, []string{qtext})
	cancel()
	if err != nil {
		return sr.degradedExactAfterEmbedFailure(ctx, sig, start, dec, toolForExact, allowed, err)
	}
	if len(vecs) != singleQueryEmbeddingCount || len(vecs[0]) != sr.dim {
		dec.Outcome = OutcomeMissInvalidEmbedding
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("%w", ErrInvalidEmbedding)
	}
	qv := vecs[0]
	store.L2Normalize(qv)

	results, err := sr.runVectorQuery(ctx, dec, start, qv, filter)
	if err != nil {
		return "", dec, err
	}

	results = sr.maybeHybridRerank(qtext, results)
	dec.Candidates = vectorHitCandidates(results, sr.cfg.HybridAlpha > 0)

	if len(results) == 0 {
		dec.Outcome = OutcomeMissNoCandidates
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "no_candidates", "latency_ms", dec.LatencyMS)
		return "", dec, fmt.Errorf("%w", ErrNoCandidates)
	}

	top := results[0]
	dec.ToolNameNamespaced = top.ToolName
	dec.UpstreamID = top.UpstreamID
	dec.Confidence = top.Score

	if err := sr.validateVectorTop(ctx, sig, start, dec, results, top); err != nil {
		return "", dec, err
	}

	dec.FallbackLayer = "vector"
	dec.Outcome = OutcomeVectorHit
	dec.LatencyMS = time.Since(start).Milliseconds()
	slog.InfoContext(ctx, "router decision", "layer", "vector", "tool", top.ToolName, "score", top.Score, "latency_ms", dec.LatencyMS, "signal", summarizeSignal(sig))
	return top.ToolName, dec, nil
}

func (sr *SemanticRouter) degradedExactAfterEmbedFailure(ctx context.Context, sig RoutingSignal, start time.Time, dec *RoutingDecision, toolForExact string, allowed []string, embedErr error) (string, *RoutingDecision, error) {
	slog.WarnContext(ctx, "router embed failed, degraded exact-only", "err", embedErr)
	if toolForExact != "" && sr.exactInCatalog(toolForExact) && sr.allowed(toolForExact, allowed) {
		dec.ToolNameNamespaced = toolForExact
		dec.UpstreamID = sr.upstreamID(toolForExact)
		dec.FallbackLayer = "degraded_exact"
		dec.Outcome = OutcomeDegradedExact
		dec.LatencyMS = time.Since(start).Milliseconds()
		return toolForExact, dec, nil
	}
	dec.Outcome = OutcomeMissDegradedNoExact
	dec.LatencyMS = time.Since(start).Milliseconds()
	return "", dec, fmt.Errorf("%w: %w", ErrDegradedNoExact, embedErr)
}

func (sr *SemanticRouter) runVectorQuery(ctx context.Context, dec *RoutingDecision, start time.Time, qv []float32, filter store.VectorSearchFilter) ([]store.VectorSearchHit, error) {
	qCtx, qCancel := context.WithTimeout(ctx, sr.cfg.QueryTimeout)
	defer qCancel()
	results, err := sr.st.Query(qCtx, qv, sr.cfg.TopK, filter)
	if err != nil {
		dec.Outcome = OutcomeMissStoreError
		dec.LatencyMS = time.Since(start).Milliseconds()
		return nil, fmt.Errorf("router: store query: %w", err)
	}
	return results, nil
}

func (sr *SemanticRouter) maybeHybridRerank(qtext string, results []store.VectorSearchHit) []store.VectorSearchHit {
	if sr.cfg.HybridAlpha > 0 && len(results) > 0 {
		return sr.hybridRerank(qtext, results)
	}
	return results
}

// tieBreakAmbiguousPair prefers the top-two vector candidate that appears most recently in session tool history.
func tieBreakAmbiguousPair(top, second store.VectorSearchHit, recent []string) (store.VectorSearchHit, store.VectorSearchHit, bool) {
	for i := len(recent) - 1; i >= 0; i-- {
		name := recent[i]
		if name == top.ToolName {
			return top, second, true
		}
		if name == second.ToolName {
			return second, top, true
		}
	}
	return top, second, false
}

func vectorHitCandidates(results []store.VectorSearchHit, hybrid bool) []ScoredTool {
	src := "vector"
	if hybrid {
		src = "bm25_hybrid"
	}
	out := make([]ScoredTool, 0, len(results))
	for _, r := range results {
		out = append(out, ScoredTool{Name: r.ToolName, Score: r.Score, Source: src})
	}
	return out
}

func (sr *SemanticRouter) validateVectorTop(ctx context.Context, sig RoutingSignal, start time.Time, dec *RoutingDecision, results []store.VectorSearchHit, top store.VectorSearchHit) error {
	if top.Score < sr.cfg.ScoreMin {
		dec.Outcome = OutcomeMissBelowThreshold
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "below_threshold", "top", top.Score, "latency_ms", dec.LatencyMS)
		return fmt.Errorf("%w: got %.4f min %.4f", ErrBelowThreshold, top.Score, sr.cfg.ScoreMin)
	}
	if len(results) > 1 && results[1].Score >= sr.cfg.ScoreMin && (results[0].Score-results[1].Score) < ambiguityScoreDeltaThreshold {
		if newTop, newSecond, ok := tieBreakAmbiguousPair(results[0], results[1], sig.RecentToolNames); ok {
			results[0], results[1] = newTop, newSecond
			top = results[0]
			slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "history_tie_break", "top", top.ToolName)
		} else {
			dec.Outcome = OutcomeMissAmbiguous
			dec.LatencyMS = time.Since(start).Milliseconds()
			slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "ambiguous", "latency_ms", dec.LatencyMS)
			return fmt.Errorf("%w", ErrAmbiguous)
		}
	}
	if !sr.cfg.AllowAutoRename && sig.ToolName != "" && top.ToolName != sig.ToolName {
		dec.Outcome = OutcomeMissRenameDisallowed
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "rename_disallowed", "requested", sig.ToolName, "winner", top.ToolName)
		return fmt.Errorf("%w: requested %q best %q", ErrRenameDisallowed, sig.ToolName, top.ToolName)
	}
	return nil
}
