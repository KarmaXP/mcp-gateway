package router

import "time"

// SemanticRouterRuntimeConfig holds in-process tuning for vector routing (timeouts, thresholds).
type SemanticRouterRuntimeConfig struct {
	Mode Mode

	TopK     int
	ScoreMin float64

	HybridAlpha float64

	AllowAutoRename bool

	EmbedTimeout time.Duration
	QueryTimeout time.Duration
}

func DefaultSemanticRouterRuntimeConfig() SemanticRouterRuntimeConfig {
	return SemanticRouterRuntimeConfig{
		Mode:            ModeOff,
		TopK:            8,
		ScoreMin:        0.35,
		HybridAlpha:     0,
		AllowAutoRename: false,
		EmbedTimeout:    10 * time.Second,
		QueryTimeout:    5 * time.Second,
	}
}
