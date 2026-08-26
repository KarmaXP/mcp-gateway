package router

import "encoding/json"

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

// AllowListAuthz mirrors gateway hostctx allow-list modes (no hostctx import in router).
type AllowListAuthz int

const (
	AllowListAuthzUnrestricted AllowListAuthz = iota
	AllowListAuthzDenyAll
	AllowListAuthzRestricted
)

type RoutingSignal struct {
	ToolName        string
	ArgumentsJSON   json.RawMessage
	IntentText      string
	AllowList       []string
	AllowListAuthz  AllowListAuthz
	CatalogVersion  string
	RecentToolNames []string
}

type RoutingDecision struct {
	Outcome            RoutingOutcome
	UpstreamID         string
	ToolNameNamespaced string
	Score              float64
	Candidates         []ScoredTool
	FallbackLayer      string
	LatencyMS          int64
}

type ScoredTool struct {
	Name   string
	Score  float64
	Source string
}
