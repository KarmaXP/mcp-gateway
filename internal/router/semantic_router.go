package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router/bm25"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

const (
	exactMatchConfidence         = 1.0
	singleQueryEmbeddingCount    = 1
	ambiguityScoreDeltaThreshold = 0.05
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
		vectorDim = defaults.VectorDimension
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
	docs := sr.toolDocStringsForHits(results)
	vecScores := vectorScoresFromHits(results)
	weights := bm25.RerankWeights(query, docs, vecScores, sr.cfg.HybridAlpha)
	return reorderHitsByWeights(results, weights)
}

func (sr *SemanticRouter) toolDocStringsForHits(results []store.VectorSearchHit) []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	docs := make([]string, len(results))
	for i, r := range results {
		if d, ok := sr.toolDoc[r.ToolName]; ok {
			docs[i] = d
		} else {
			docs[i] = r.ToolName
		}
	}
	return docs
}

func vectorScoresFromHits(results []store.VectorSearchHit) []float64 {
	vecScores := make([]float64, len(results))
	for i, r := range results {
		vecScores[i] = r.Score
	}
	return vecScores
}

func reorderHitsByWeights(results []store.VectorSearchHit, weights []float64) []store.VectorSearchHit {
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
	return buildIndexedToolsFromRows(rows, upstreamIDForPrefix)
}

// BuildIndexedToolsFromMerged builds the semantic-router catalog from merged tools/list maps without
// re-marshaling or re-parsing list JSON (avoids redundant work on the tools/list hot path).
func BuildIndexedToolsFromMerged(merged []map[string]any, upstreamIDForPrefix func(prefix string) (string, error)) ([]IndexedTool, error) {
	return buildIndexedToolsFromRows(index.ToolRowsFromListMaps(merged), upstreamIDForPrefix)
}

func buildIndexedToolsFromRows(rows []index.ToolRow, upstreamIDForPrefix func(prefix string) (string, error)) ([]IndexedTool, error) {
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
