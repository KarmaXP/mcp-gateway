package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/orchestrator"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func routerModeActive(cfg config.GatewayConfig) bool {
	mode := strings.ToLower(strings.TrimSpace(cfg.SemanticRouter.Mode))
	return mode == "on" || mode == "assist_list" || mode == "filter_list"
}

func preflightQdrant(cfg config.GatewayConfig) {
	if !routerModeActive(cfg) {
		return
	}
	qURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaults.PreflightQdrantTimeout)
	defer cancel()
	if err := store.PingCollections(ctx, qURL); err != nil {
		slog.Warn("qdrant preflight failed (continuing; router/index may fail until Qdrant is healthy)",
			"err", err,
			"url", qURL,
		)
		return
	}
	slog.Info("qdrant preflight ok", "url", qURL)
}

func multiplexerOptions(cfg config.GatewayConfig) ([]multiplex.Option, error) {
	opts := []multiplex.Option{
		multiplex.WithListTTL(0),
		multiplex.WithInitTimeout(cfg.AggregationInitTimeout()),
		multiplex.WithListTimeout(cfg.AggregationListTimeout()),
		multiplex.WithCallTimeout(cfg.AggregationCallTimeout()),
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.SemanticRouter.Mode))
	if mode == "" {
		mode = string(router.ModeOff)
	}
	if mode != "on" && mode != "assist_list" && mode != "filter_list" {
		return opts, nil
	}

	embedURL := strings.TrimSpace(cfg.Embedding.URL)
	if embedURL == "" {
		embedURL = defaults.DefaultEmbedServiceURL
	}
	dim := cfg.SemanticRouter.VectorDim
	if dim <= 0 {
		dim = defaults.VectorDimension
	}

	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	switch mode {
	case "on":
		rcfg.Mode = router.ModeOn
	case "filter_list":
		rcfg.Mode = router.ModeFilterList
	default:
		rcfg.Mode = router.ModeAssistList
	}
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

	qURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
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

func main() {
	ctx := context.Background()
	teleShutdown, err := telemetry.Init(ctx, defaults.DefaultTelemetryServiceName)
	if err != nil {
		slog.Error("telemetry init", "err", err)
		os.Exit(1)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), defaults.TelemetryShutdownTimeout)
		defer cancel()
		if err := teleShutdown(sctx); err != nil {
			slog.Error("telemetry shutdown", "err", err)
		}
	}()

	baseLog := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(telemetry.TraceHandler(baseLog)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	preflightQdrant(cfg)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = strings.TrimSpace(os.Getenv("GATEWAY_PORT"))
	}
	if port == "" {
		port = defaults.DefaultGatewayHTTPPort
	}
	addr := ":" + port

	authCfg := auth.JWTAuthFromEnvironment()
	validator, err := auth.NewValidator(authCfg)
	if err != nil {
		slog.Error("auth config", "err", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	polHolder := policy.NewHolder(policy.NewEngine(cfg.Policy))

	upstreams, cleanupUpstreams, err := backend.ConnectUpstreams(rootCtx, cfg.Upstreams)
	if err != nil {
		slog.Error("connect upstreams", "err", err)
		os.Exit(1)
	}
	defer cleanupUpstreams()

	mpxOpts, err := multiplexerOptions(cfg)
	if err != nil {
		slog.Error("multiplexer options", "err", err)
		os.Exit(1)
	}
	mpxOpts = append(mpxOpts, multiplex.WithPolicyHolder(polHolder))
	mpxOpts = append(mpxOpts, multiplex.WithAggregationStrict(cfg.Aggregation.StrictInitialize, cfg.Aggregation.StrictList))
	mpxOpts = append(mpxOpts, multiplex.WithReportPartialFailures(cfg.Aggregation.ReportPartialFailures))
	mpx, err := multiplex.New(upstreams, mpxOpts...)
	if err != nil {
		slog.Error("multiplexer", "err", err)
		os.Exit(1)
	}

	httpOpts := orchestrator.HTTPServerOptions(defaults.DefaultTelemetryServiceName, authCfg, validator, polHolder, ratelimit.FromEnvironment())
	httpOpts = append(httpOpts, httpserver.WithShutdownContext(rootCtx))
	srv := httpserver.New(mpx, addr, httpOpts...)

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGHUP)
		for range ch {
			cfg2, err := config.Load()
			if err != nil {
				slog.Error("policy reload skipped: config load failed", "err", err)
				continue
			}
			policy.ReloadEngine(polHolder, cfg2)
			slog.Info("policy reloaded from config", "policy_version", cfg2.Policy.Version)
		}
	}()

	go func() {
		slog.Info("mcp-gateway listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			stop()
		}
	}()

	<-rootCtx.Done()
	slog.Info("shutdown signal received")

	sctx, cancel := context.WithTimeout(context.Background(), defaults.HTTPServerShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		slog.Error("http shutdown", "err", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
