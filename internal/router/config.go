package router

import (
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

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
		TopK:            defaults.RouterTopK,
		ScoreMin:        defaults.RouterScoreMin,
		HybridAlpha:     0,
		AllowAutoRename: false,
		EmbedTimeout:    defaults.RouterEmbedTimeout,
		QueryTimeout:    defaults.RouterQueryTimeout,
	}
}
