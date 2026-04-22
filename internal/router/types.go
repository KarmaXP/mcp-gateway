// Package router implements semantic routing (intent/signal → tool decision) before multiplex dispatch.
package router

import "encoding/json"

// Mode controls whether vector routing runs.
type Mode string

const (
	// ModeOff disables embeddings and vector search; multiplexor uses exact namespaced names only.
	ModeOff Mode = "off"
	// ModeAssistList keeps a full tools/list to the host; routing applies on tools/call only.
	ModeAssistList Mode = "assist_list"
)

// RoutingOutcome is a low-cardinality classifier for metrics and logs; always set on RoutingDecision, including errors.
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

// RoutingSignal is the per-request router input.
type RoutingSignal struct {
	SessionID      string
	Method         string
	ToolName       string
	ArgumentsJSON  json.RawMessage
	IntentText     string
	AllowedTools   []string // namespaced names; empty means no allow-list filter
	CatalogVersion string
}

// RoutingDecision is the router output consumed by the orchestrator.
type RoutingDecision struct {
	Outcome            RoutingOutcome
	BackendID          string
	ToolNameNamespaced string
	Confidence         float64
	Candidates         []ScoredTool
	FallbackLayer      string // "exact" | "vector" | "degraded_exact" | "none" | "vector" (attempted)
	LatencyMS          int64
}

// ScoredTool is one ranked neighbour for logs and debugging.
type ScoredTool struct {
	Name   string
	Score  float64
	Source string // "vector", "exact"
}
