package app

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func multiplexOptions(cfg config.GatewayConfig, getenv func(string) string) ([]multiplex.Option, error) {
	opts := []multiplex.Option{
		multiplex.WithListTTL(cfg.AggregationListCacheTTL()),
		multiplex.WithInitTimeout(cfg.AggregationInitTimeout()),
		multiplex.WithListTimeout(cfg.AggregationListTimeout()),
		multiplex.WithCallTimeout(cfg.AggregationCallTimeout()),
		multiplex.WithGlobalMaxInFlight(cfg.AggregationMaxInFlight()),
	}
	if !routerModeActive(cfg) {
		return opts, nil
	}
	mode, _ := mode.Parse(cfg.SemanticRouter.Mode)

	embedURL := strings.TrimSpace(cfg.Embedding.URL)
	if embedURL == "" {
		embedURL = defaults.DefaultEmbedServiceURL
	}
	dim := cfg.SemanticRouter.VectorDim
	if dim <= 0 {
		dim = defaults.VectorDimension
	}

	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = mode
	if cfg.SemanticRouter.TopK > 0 {
		rcfg.TopK = cfg.SemanticRouter.TopK
	}
	if cfg.SemanticRouter.ScoreMin > 0 {
		rcfg.ScoreMin = cfg.SemanticRouter.ScoreMin
	}
	if cfg.SemanticRouter.HybridAlpha > 0 {
		rcfg.HybridAlpha = cfg.SemanticRouter.HybridAlpha
	}
	if cfg.SemanticRouter.AllowAutoRename {
		rcfg.AllowAutoRename = true
	}
	rcfg.EmbedTimeout = cfg.RouterEmbedTimeout()
	rcfg.QueryTimeout = cfg.RouterQueryTimeout()

	qURL := strings.TrimSpace(getenv("QDRANT_URL"))
	if qURL == "" {
		return nil, fmt.Errorf("QDRANT_URL is required when router mode is on, assist_list, or filter_list")
	}
	st, err := store.NewQdrantVectorStore(qURL, cfg.QdrantCollection(), dim)
	if err != nil {
		return nil, err
	}

	sr := router.NewSemanticRouter(rcfg, embed.NewClient(embedURL), st, dim)
	if len(cfg.SemanticRouter.Rules.Aliases) > 0 || len(cfg.SemanticRouter.Rules.SiloKeywords) > 0 {
		sr.SetRules(rules.New(cfg.SemanticRouter.Rules.Aliases, cfg.SemanticRouter.Rules.SiloKeywords))
	}
	opts = append(opts, multiplex.WithSemanticRouter(sr))
	slog.Info("semantic router enabled",
		"embed_url", embedURL,
		"vector_dim", dim,
		"score_min", rcfg.ScoreMin,
		"top_k", rcfg.TopK,
		"hybrid_alpha", rcfg.HybridAlpha,
		"allow_auto_rename", rcfg.AllowAutoRename,
		"qdrant_collection", cfg.QdrantCollection(),
	)
	return opts, nil
}
