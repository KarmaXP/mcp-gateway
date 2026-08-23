package router

import (
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

type SemanticRouterRuntimeConfig struct {
	Mode mode.Mode

	TopK     int
	ScoreMin float64

	HybridAlpha float64

	AllowAutoRename bool

	EmbedTimeout time.Duration
	QueryTimeout time.Duration
}

func DefaultSemanticRouterRuntimeConfig() SemanticRouterRuntimeConfig {
	return SemanticRouterRuntimeConfig{
		Mode:            mode.Off,
		TopK:            defaults.RouterTopK,
		ScoreMin:        defaults.RouterScoreMin,
		HybridAlpha:     0,
		AllowAutoRename: false,
		EmbedTimeout:    defaults.RouterEmbedTimeout,
		QueryTimeout:    defaults.RouterQueryTimeout,
	}
}
