package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// CatalogEntry is one tool row plus backend id for indexing.
type CatalogEntry struct {
	ToolRow   index.ToolRow
	BackendID string
}

// Engine runs the Signal→Decision pipeline (plan §3.B).
type Engine struct {
	cfg      Config
	embedder embed.Embedder
	st       store.Store
	dim      int

	mu            sync.RWMutex
	catalog       map[string]struct{} // exact namespaced tool names
	catalogVer    string
	backendByTool map[string]string // toolName -> backend id
}

// NewEngine constructs a router engine. embedder may be nil only when ModeOff.
func NewEngine(cfg Config, e embed.Embedder, st store.Store, vectorDim int) *Engine {
	if vectorDim <= 0 {
		vectorDim = 384
	}
	return &Engine{
		cfg:           cfg,
		embedder:      e,
		st:            st,
		dim:           vectorDim,
		catalog:       make(map[string]struct{}),
		backendByTool: make(map[string]string),
	}
}

// Enabled reports whether vector routing is active.
func (e *Engine) Enabled() bool {
	return e != nil && e.cfg.Mode != ModeOff
}

// CatalogVersion returns the active indexed catalog hash.
func (e *Engine) CatalogVersion() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.catalogVer
}

// Reindex rebuilds the vector index from aggregated tools/list (plan §3.B.3 Phase 1).
func (e *Engine) Reindex(ctx context.Context, version string, entries []CatalogEntry) error {
	if e == nil || !e.Enabled() {
		return nil
	}
	if e.embedder == nil || e.st == nil {
		return fmt.Errorf("router: embedder or store nil: %w", errNilDeps)
	}
	if version == "" {
		return fmt.Errorf("router: empty catalog version")
	}

	docs := make([]string, len(entries))
	points := make([]store.Point, 0, len(entries))
	for i, ent := range entries {
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
		embCtx, cancel := context.WithTimeout(ctx, e.cfg.EmbedTimeout)
		vecs, err := e.embedder.Embed(embCtx, chunk)
		cancel()
		if err != nil {
			return fmt.Errorf("router: embed batch: %w", err)
		}
		all = append(all, vecs...)
	}
	if len(all) != len(entries) {
		return fmt.Errorf("router: embed count mismatch")
	}

	for i, ent := range entries {
		v := all[i]
		if len(v) != e.dim {
			return fmt.Errorf("router: vector dim %d want %d", len(v), e.dim)
		}
		store.L2Normalize(v)
		id := fmt.Sprintf("%s::%s", version, ent.ToolRow.Name)
		points = append(points, store.Point{
			ID:       id,
			Vector:   v,
			ToolName: ent.ToolRow.Name,
			Backend:  ent.BackendID,
			Version:  version,
		})
	}

	if err := e.st.Upsert(ctx, points); err != nil {
		return fmt.Errorf("router: upsert: %w", err)
	}

	e.mu.Lock()
	e.catalogVer = version
	e.catalog = make(map[string]struct{}, len(entries))
	e.backendByTool = make(map[string]string, len(entries))
	for _, ent := range entries {
		e.catalog[ent.ToolRow.Name] = struct{}{}
		e.backendByTool[ent.ToolRow.Name] = ent.BackendID
	}
	e.mu.Unlock()

	slog.InfoContext(ctx, "router catalog reindexed", "catalog_version", version, "tools", len(entries))
	return nil
}

// ResolveToolsCall returns the namespaced tool name to use for §3.A dispatch.
func (e *Engine) ResolveToolsCall(ctx context.Context, sig RoutingSignal) (string, *RoutingDecision, error) {
	if e == nil || !e.Enabled() {
		return sig.ToolName, &RoutingDecision{FallbackLayer: "none", Outcome: OutcomeNone}, nil
	}
	start := time.Now()
	dec := &RoutingDecision{FallbackLayer: "vector"}

	// Optional host-supplied catalog pin (future §3.C headers); empty skips check.
	if sig.CatalogVersion != "" && e.CatalogVersion() != "" && sig.CatalogVersion != e.CatalogVersion() {
		dec.Outcome = OutcomeMissStaleCatalog
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("%w: client %q vs server %q", ErrStaleCatalog, sig.CatalogVersion, e.CatalogVersion())
	}

	allowed := sig.AllowedTools
	filter := store.Filter{CatalogVersion: e.CatalogVersion(), AllowedTools: allowed}

	// S3 — deterministic shortcut
	if sig.ToolName != "" && e.exactInCatalog(sig.ToolName) && e.allowed(sig.ToolName, allowed) {
		bid := e.backendID(sig.ToolName)
		dec.BackendID = bid
		dec.ToolNameNamespaced = sig.ToolName
		dec.Confidence = 1
		dec.FallbackLayer = "exact"
		dec.Outcome = OutcomeExact
		dec.Candidates = []ScoredTool{{Name: sig.ToolName, Score: 1, Source: "exact"}}
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "exact", "tool", sig.ToolName, "latency_ms", dec.LatencyMS)
		return sig.ToolName, dec, nil
	}

	qtext := index.FormatQuery(sig.ToolName, sig.IntentText, jsonKeys(sig.ArgumentsJSON))
	embCtx, cancel := context.WithTimeout(ctx, e.cfg.EmbedTimeout)
	vecs, err := e.embedder.Embed(embCtx, []string{qtext})
	cancel()
	if err != nil {
		slog.WarnContext(ctx, "router embed failed, degraded exact-only", "err", err)
		if sig.ToolName != "" && e.exactInCatalog(sig.ToolName) && e.allowed(sig.ToolName, allowed) {
			dec.ToolNameNamespaced = sig.ToolName
			dec.BackendID = e.backendID(sig.ToolName)
			dec.FallbackLayer = "degraded_exact"
			dec.Outcome = OutcomeDegradedExact
			dec.LatencyMS = time.Since(start).Milliseconds()
			return sig.ToolName, dec, nil
		}
		dec.Outcome = OutcomeMissDegradedNoExact
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("%w: %w", ErrDegradedNoExact, err)
	}
	if len(vecs) != 1 || len(vecs[0]) != e.dim {
		dec.Outcome = OutcomeMissInvalidEmbedding
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("%w", ErrInvalidEmbedding)
	}
	qv := vecs[0]
	store.L2Normalize(qv)

	qCtx, qCancel := context.WithTimeout(ctx, e.cfg.QueryTimeout)
	results, err := e.st.Query(qCtx, qv, e.cfg.TopK, filter)
	qCancel()
	if err != nil {
		dec.Outcome = OutcomeMissStoreError
		dec.LatencyMS = time.Since(start).Milliseconds()
		return "", dec, fmt.Errorf("router: store query: %w", err)
	}
	for _, r := range results {
		dec.Candidates = append(dec.Candidates, ScoredTool{Name: r.ToolName, Score: r.Score, Source: "vector"})
	}
	if len(results) == 0 {
		dec.Outcome = OutcomeMissNoCandidates
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "no_candidates", "latency_ms", dec.LatencyMS)
		return "", dec, fmt.Errorf("%w", ErrNoCandidates)
	}

	top := results[0]
	dec.ToolNameNamespaced = top.ToolName
	dec.BackendID = top.Backend
	dec.Confidence = top.Score

	if top.Score < e.cfg.ScoreMin {
		dec.Outcome = OutcomeMissBelowThreshold
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "below_threshold", "top", top.Score, "latency_ms", dec.LatencyMS)
		return "", dec, fmt.Errorf("%w: got %.4f min %.4f", ErrBelowThreshold, top.Score, e.cfg.ScoreMin)
	}
	if len(results) > 1 && results[1].Score >= e.cfg.ScoreMin && (results[0].Score-results[1].Score) < 0.05 {
		dec.Outcome = OutcomeMissAmbiguous
		dec.LatencyMS = time.Since(start).Milliseconds()
		slog.InfoContext(ctx, "router decision", "layer", "vector", "outcome", "ambiguous", "latency_ms", dec.LatencyMS)
		return "", dec, fmt.Errorf("%w", ErrAmbiguous)
	}

	if !e.cfg.AllowAutoRename && sig.ToolName != "" && top.ToolName != sig.ToolName {
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

func summarizeSignal(sig RoutingSignal) string {
	// SEC5 / O3: no argument payloads — tool name and intent length only
	return fmt.Sprintf("tool_len=%d intent_len=%d", len(sig.ToolName), len(sig.IntentText))
}

func (e *Engine) exactInCatalog(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.catalog[name]
	return ok
}

func (e *Engine) backendID(name string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.backendByTool[name]
}

func (e *Engine) allowed(tool string, allowed []string) bool {
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

// BuildCatalogEntries maps tools/list JSON + prefix→backendID into router entries.
func BuildCatalogEntries(listJSON []byte, backendForPrefix func(prefix string) (string, error)) ([]CatalogEntry, error) {
	rows, err := index.ParseToolsListJSON(listJSON)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogEntry, 0, len(rows))
	for _, r := range rows {
		prefix, _, err := namespace.Split(r.Name)
		if err != nil {
			slog.Warn("router skip tool for index", "tool", r.Name, "err", err)
			continue
		}
		bid, err := backendForPrefix(prefix)
		if err != nil {
			return nil, err
		}
		out = append(out, CatalogEntry{ToolRow: r, BackendID: bid})
	}
	return out, nil
}
