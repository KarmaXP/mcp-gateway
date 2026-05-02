package router

import "encoding/json"

type Mode string

const (
	ModeOff        Mode = "off"
	ModeOn         Mode = "on"
	ModeAssistList Mode = "assist_list"
	ModeFilterList Mode = "filter_list"
)

type RoutingOutcome string

const (
	OutcomeNone                 RoutingOutcome = "none"
	OutcomeExact                RoutingOutcome = "exact"
	OutcomeRulesAlias           RoutingOutcome = "rules_alias"
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

type RoutingSignal struct {
	SessionID      string
	Method         string
	ToolName       string
	ArgumentsJSON  json.RawMessage
	IntentText     string
	AllowedTools   []string
	CatalogVersion string
}

type RoutingDecision struct {
	Outcome            RoutingOutcome
	UpstreamID         string
	ToolNameNamespaced string
	Confidence         float64
	Candidates         []ScoredTool
	FallbackLayer      string
	LatencyMS          int64
}

type ScoredTool struct {
	Name   string
	Score  float64
	Source string
}
