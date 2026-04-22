// Package router implements the semantic router (§3.B): Signal→Decision before multiplexor dispatch.
package router

import "encoding/json"

// Mode controls whether vector routing runs (plan §3.B operating modes).
type Mode string

const (
	// ModeOff disables embeddings and vector search; multiplexor uses exact namespaced names only.
	ModeOff Mode = "off"
	// ModeAssistList keeps a full tools/list to the host; routing applies on tools/call only.
	ModeAssistList Mode = "assist_list"
)

// RoutingOutcome is a stable classifier for observability and policy (bounded cardinality, O5).
// It is always set on RoutingDecision, including on error paths, so callers can emit metrics without parsing errors.
type RoutingOutcome string

const (
	OutcomeNone                 RoutingOutcome = "none"
	OutcomeExact                RoutingOutcome = "exact"
	OutcomeVectorHit            RoutingOutcome = "vector_hit"
	OutcomeDegradedExact        RoutingOutcome = "degraded_exact"
	OutcomeMissStaleCatalog     RoutingOutcome = "miss_stale_catalog"
	OutcomeMissNoCandidates     RoutingOutcome = "miss_no_candidates"
	OutcomeMissBelowThreshold   RoutingOutcome = "miss_below_threshold"
	OutcomeMissAmbiguous        RoutingOutcome = "miss_ambiguous"
	OutcomeMissRenameDisallowed RoutingOutcome = "miss_rename_disallowed"
	OutcomeMissDegradedNoExact  RoutingOutcome = "miss_degraded_no_exact"
	OutcomeMissInvalidEmbedding RoutingOutcome = "miss_invalid_embedding"
	OutcomeMissStoreError       RoutingOutcome = "miss_store_error"
)

// RoutingSignal is the per-request input to the router (plan §3.B.2).
type RoutingSignal struct {
	SessionID      string
	Method         string
	ToolName       string
	ArgumentsJSON  json.RawMessage
	IntentText     string
	AllowedTools   []string // namespaced names; empty = all tools in current catalog version (until §3.C)
	CatalogVersion string
}

// RoutingDecision is the router output consumed by the orchestrator (plan §3.B.2).
type RoutingDecision struct {
	Outcome            RoutingOutcome
	BackendID          string
	ToolNameNamespaced string
	Confidence         float64
	Candidates         []ScoredTool
	FallbackLayer      string // "exact" | "vector" | "degraded_exact" | "none" | "vector" (attempted)
	LatencyMS          int64
}

// ScoredTool is one candidate for explainability logs (plan §3.B / S7).
type ScoredTool struct {
	Name   string
	Score  float64
	Source string // "vector", "exact"
}
