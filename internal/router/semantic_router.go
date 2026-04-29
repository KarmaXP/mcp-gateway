package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router/bm25"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// IndexedTool is one catalog tool row plus the upstream that owns its namespace prefix.
type IndexedTool struct {
	ToolRow    index.ToolRow
	UpstreamID string
}

// SemanticRouter resolves tools/call to a namespaced tool and upstream using exact match, rules, and vector search.
type SemanticRouter struct {
	cfg      SemanticRouterRuntimeConfig
	embedder embed.Embedder
	st       store.Store
	dim      int

	mu             sync.RWMutex
	catalog        map[string]struct{}
	catalogVer     string
	upstreamByTool map[string]string
	toolDoc        map[string]string

	rules *rules.Rules
}

func NewSemanticRouter(cfg SemanticRouterRuntimeConfig, e embed.Embedder, st store.Store, vectorDim int) *SemanticRouter {
	if vectorDim <= 0 {
		vectorDim = 384
	}
	return &SemanticRouter{
		cfg:            cfg,
		embedder:       e,
		st:             st,
		dim:            vectorDim,
		catalog:        make(map[string]struct{}),
		upstreamByTool: make(map[string]string),
		toolDoc:        make(map[string]string),
	}
}

func (sr *SemanticRouter) SetRules(r *rules.Rules) {
	if sr == nil {
		return
	}
	sr.mu.Lock()
	sr.rules = r
	sr.mu.Unlock()
}

func (sr *SemanticRouter) Enabled() bool {
	return sr != nil && sr.cfg.Mode != ModeOff
}

func (sr *SemanticRouter) CatalogVersion() string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.catalogVer
}

func (sr *SemanticRouter) Reindex(ctx context.Context, version string, tools []IndexedTool) error {
	if sr == nil || !sr.Enabled() {
		return nil
	}
	if sr.embedder == nil || sr.st == nil {
		return fmt.Errorf("router: embedder or store nil: %w", errNilDeps)
	}
	if version == "" {
		return fmt.Errorf("router: empty catalog version")
	}

	docs := make([]string, len(tools))
	records := make([]store.ToolVectorRecord, 0, len(tools))
	for i, ent := range tools {
		docs[i] = index.FormatDocument(ent.ToolRow)
	}

	batch := 64
	var all [][]float32
	for i := 0; i < len(docs); i += batch {
		j := i + batch
		if j > len(docs) {
			j = len(docs)
		}
		chunk := docs[i:j]
		embCtx, cancel := context.WithTimeout(ctx, sr.cfg.EmbedTimeout)
		vecs, err := sr.embedder.Embed(embCtx, chunk)
		cancel()
		if err != nil {
			return fmt.Errorf("router: embed batch: %w", err)
		}
		all = append(all, vecs...)
	}
	if len(all) != len(tools) {
		return fmt.Errorf("router: embed count mismatch")
	}

	for i, ent := range tools {
		v := all[i]
		if len(v) != sr.dim {
			return fmt.Errorf("router: vector dim %d want %d", len(v), sr.dim)
		}
		store.L2Normalize(v)
		id := fmt.Sprintf("%s::%s", version, ent.ToolRow.Name)
		records = append(records, store.ToolVectorRecord{
			ID:             id,
			Vector:         v,
			ToolName:       ent.ToolRow.Name,
			UpstreamID:     ent.UpstreamID,
			CatalogVersion: version,
		})
	}

	if err := sr.st.Upsert(ctx, records); err != nil {
		return fmt.Errorf("router: upsert: %w", err)
	}

	sr.mu.Lock()
	sr.catalogVer = version
	sr.catalog = make(map[string]struct{}, len(tools))
	sr.upstreamByTool = make(map[string]string, len(tools))
	sr.toolDoc = make(map[string]string, len(tools))
	for _, ent := range tools {
		sr.catalog[ent.ToolRow.Name] = struct{}{}
		sr.upstreamByTool[ent.ToolRow.Name] = ent.UpstreamID
		sr.toolDoc[ent.ToolRow.Name] = index.FormatDocument(ent.ToolRow)
	}
	sr.mu.Unlock()

	slog.InfoContext(ctx, "router catalog reindexed", "catalog_version", version, "tools", len(tools))
	return nil
}

func (sr *SemanticRouter) ResolveToolsCall(ctx context.Context, sig RoutingSignal) (string, *RoutingDecision, error) {
	if sr == nil || !sr.Enabled() {
		return sig.ToolName, &RoutingDecision{FallbackLayer: "none", Outcome: OutcomeNone}, nil
	}
	start := time.Now()
	dec := &RoutingDecision{FallbackLayer: "vector"}

	if sig.CatalogVersion != "" && sr.CatalogVersion() != "" && sig.CatalogVersion != sr.CatalogVersion() {
		dec.Outcome = OutcomeMissStaleCatalog
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("%w: client %q vs server %q", ErrStaleCatalog, sig.CatalogVersion, sr.CatalogVersion())
	}

	allowed := append([]string(nil), sig.AllowedTools...)
	sr.mu.RLock()
	rl := sr.rules
	sr.mu.RUnlock()
	if rl != nil {
		allowed = rl.NarrowAllowed(sig.IntentText, allowed, sr.listCatalog())
	}

	toolForExact := sig.ToolName
	if rl != nil {
		if c := rl.CanonicalAlias(sig.ToolName); c != "" {
			toolForExact = c
		}
	}

	filter := store.VectorSearchFilter{CatalogVersion: sr.CatalogVersion(), AllowedToolNames: allowed}

	if toolForExact != "" && sr.exactInCatalog(toolForExact) && sr.allowed(toolForExact, allowed) {
		uid := sr.upstreamID(toolForExact)
		dec.UpstreamID = uid
		dec.ToolNameNamespaced = toolForExact
		dec.Confidence = 1
		if rl != nil && strings.TrimSpace(sig.ToolName) != "" && toolForExact != sig.ToolName {
			dec.FallbackLayer = "rules"
			dec.Outcome = OutcomeRulesAlias
			dec.Candidates = []ScoredTool{{Name: toolForExact, Score: 1, Source: "rules"}}
		} else {
			dec.FallbackLayer = "exact"
			dec.Outcome = OutcomeExact
			dec.Candidates = []ScoredTool{{Name: toolForExact, Score: 1, Source: "exact"}}
		}
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", dec.FallbackLayer, "tool", toolForExact, "latency_ms", dec.LatencyMS)
		return toolForExact, dec, nil
	}

	qtext := index.FormatQuery(sig.ToolName, sig.IntentText, jsonKeys(sig.ArgumentsJSON))
	embCtx, cancel := context.WithTimeout(ctx, sr.cfg.EmbedTimeout)
	vecs, err := sr.embedder.Embed(embCtx, []string{qtext})
	cancel()
	if err != nil {
		slog.WarnContext(ctx, "router embed failed, degraded exact-only", "err", err)
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
		return "", dec, fmt.Errorf("%w: %w", ErrDegradedNoExact, err)
	}
	if len(vecs) != 1 || len(vecs[0]) != sr.dim {
		dec.Outcome = OutcomeMissInvalidEmbedding
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("%w", ErrInvalidEmbedding)
	}
	qv := vecs[0]
	store.L2Normalize(qv)

	qCtx, qCancel := context.WithTimeout(ctx, sr.cfg.QueryTimeout)
	results, err := sr.st.Query(qCtx, qv, sr.cfg.TopK, filter)
	qCancel()
	if err != nil {
		dec.Outcome = OutcomeMissStoreError
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("router: store query: %w", err)
	}
	if sr.cfg.HybridAlpha > 0 && len(results) > 0 {
		results = sr.hybridRerank(qtext, results)
	}
	src := "vector"
	if sr.cfg.HybridAlpha > 0 {
		src = "bm25_hybrid"
	}
	for _, r := range results {
		dec.Candidates = append(dec.Candidates, ScoredTool{Name: r.ToolName, Score: r.Score, Source: src})
	}
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

	if top.Score < sr.cfg.ScoreMin {
		dec.Outcome = OutcomeMissBelowThreshold
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "below_threshold", "top", top.Score, "latency_ms", dec.LatencyMS)
		return "", dec, fmt.Errorf("%w: got %.4f min %.4f", ErrBelowThreshold, top.Score, sr.cfg.ScoreMin)
	}
	if len(results) > 1 && results[1].Score >= sr.cfg.ScoreMin && (results[0].Score-results[1].Score) < 0.05 {
		dec.Outcome = OutcomeMissAmbiguous
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "ambiguous", "latency_ms", dec.LatencyMS)
		return "", dec, fmt.Errorf("%w", ErrAmbiguous)
	}

	if !sr.cfg.AllowAutoRename && sig.ToolName != "" && top.ToolName != sig.ToolName {
		dec.Outcome = OutcomeMissRenameDisallowed
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "rename_disallowed", "requested", sig.ToolName, "winner", top.ToolName)
		return "", dec, fmt.Errorf("%w: requested %q best %q", ErrRenameDisallowed, sig.ToolName, top.ToolName)
	}

	dec.FallbackLayer = "vector"
	dec.Outcome = OutcomeVectorHit
	dec.LatencyMS = time.Since(start).Milliseconds()
	slog.InfoContext(ctx, "router decision", "layer", "vector", "tool", top.ToolName, "score", top.Score, "latency_ms", dec.LatencyMS, "signal", summarizeSignal(sig))
	return top.ToolName, dec, nil
}

func (sr *SemanticRouter) listCatalog() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	out := make([]string, 0, len(sr.catalog))
	for k := range sr.catalog {
		out = append(out, k)
	}
	return out
}

type hybridPair struct {
	res store.VectorSearchHit
	w   float64
}

func (sr *SemanticRouter) hybridRerank(query string, results []store.VectorSearchHit) []store.VectorSearchHit {
	if sr == nil || sr.cfg.HybridAlpha <= 0 || len(results) == 0 {
		return results
	}
	sr.mu.RLock()
	docs := make([]string, len(results))
	for i, r := range results {
		if d, ok := sr.toolDoc[r.ToolName]; ok {
			docs[i] = d
		} else {
			docs[i] = r.ToolName
		}
	}
	sr.mu.RUnlock()
	vecScores := make([]float64, len(results))
	for i, r := range results {
		vecScores[i] = r.Score
	}
	weights := bm25.RerankWeights(query, docs, vecScores, sr.cfg.HybridAlpha)
	pairs := make([]hybridPair, len(results))
	for i := range results {
		pairs[i] = hybridPair{res: results[i], w: weights[i]}
	}
	sort.Slice(pairs, func(a, b int) bool { return pairs[a].w > pairs[b].w })
	out := make([]store.VectorSearchHit, len(pairs))
	for i := range pairs {
		r := pairs[i].res
		r.Score = pairs[i].w
		out[i] = r
	}
	return out
}

func summarizeSignal(sig RoutingSignal) string {
	return fmt.Sprintf("tool_len=%d intent_len=%d", len(sig.ToolName), len(sig.IntentText))
}

func (sr *SemanticRouter) exactInCatalog(name string) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	_, ok := sr.catalog[name]
	return ok
}

func (sr *SemanticRouter) upstreamID(name string) string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.upstreamByTool[name]
}

func (sr *SemanticRouter) allowed(tool string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == tool {
			return true
		}
	}
	return false
}

func jsonKeys(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// BuildIndexedTools parses a tools/list JSON payload and attaches upstream IDs from namespace prefixes.
func BuildIndexedTools(listJSON []byte, upstreamIDForPrefix func(prefix string) (string, error)) ([]IndexedTool, error) {
	rows, err := index.ParseToolsListJSON(listJSON)
	if err != nil {
		return nil, err
	}
	out := make([]IndexedTool, 0, len(rows))
	for _, r := range rows {
		prefix, _, err := namespace.Split(r.Name)
		if err != nil {
			slog.Warn("router skip tool for index", "tool", r.Name, "err", err)
			continue
		}
		uid, err := upstreamIDForPrefix(prefix)
		if err != nil {
			return nil, err
		}
		out = append(out, IndexedTool{ToolRow: r, UpstreamID: uid})
	}
	return out, nil
}
