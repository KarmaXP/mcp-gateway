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
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/orchestrator"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func routerModeActive(cfg config.Config) bool {
	mode := strings.ToLower(strings.TrimSpace(cfg.Router.Mode))
	return mode == "on" || mode == "assist_list"
}

// preflightQdrant logs reachability when the semantic router is enabled; does not exit on failure.
func preflightQdrant(cfg config.Config) {
	if !routerModeActive(cfg) {
		return
	}
	qURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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

func aggregatorOptions(cfg config.Config) ([]aggregate.Option, error) {
	opts := []aggregate.Option{aggregate.WithListTTL(0)}
	mode := strings.ToLower(strings.TrimSpace(cfg.Router.Mode))
	if mode == "" {
		mode = "off"
	}
	if mode != "on" && mode != "assist_list" {
		return opts, nil
	}

	embedURL := strings.TrimSpace(cfg.Embed.URL)
	if embedURL == "" {
		embedURL = "http://127.0.0.1:8001"
	}
	dim := cfg.Router.VectorDim
	if dim <= 0 {
		dim = 384
	}

	rcfg := router.DefaultConfig()
	rcfg.Mode = router.ModeAssistList
	if cfg.Router.TopK > 0 {
		rcfg.TopK = cfg.Router.TopK
	}
	if cfg.Router.ScoreMin > 0 {
		rcfg.ScoreMin = cfg.Router.ScoreMin
	}
	if cfg.Router.AllowAutoRename {
		rcfg.AllowAutoRename = true
	}
	rcfg.EmbedTimeout = cfg.RouterEmbedTimeout()
	rcfg.QueryTimeout = cfg.RouterQueryTimeout()

	qURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qURL == "" {
		return nil, fmt.Errorf("QDRANT_URL is required when router mode is on or assist_list")
	}
	st, err := store.NewQdrant(qURL, cfg.QdrantCollection(), dim)
	if err != nil {
		return nil, err
	}

	e := router.NewEngine(rcfg, embed.NewClient(embedURL), st, dim)
	opts = append(opts, aggregate.WithSemanticRouter(e))
	slog.Info("semantic router enabled",
		"embed_url", embedURL,
		"vector_dim", dim,
		"score_min", rcfg.ScoreMin,
		"top_k", rcfg.TopK,
		"allow_auto_rename", rcfg.AllowAutoRename,
		"qdrant_collection", cfg.QdrantCollection(),
	)
	return opts, nil
}

func main() {
	ctx := context.Background()
	teleShutdown, err := telemetry.Init(ctx, "mcp-gateway")
	if err != nil {
		slog.Error("telemetry init", "err", err)
		os.Exit(1)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
		port = "8080"
	}
	addr := ":" + port

	authCfg := auth.ConfigFromEnv()
	validator, err := auth.NewValidator(authCfg)
	if err != nil {
		slog.Error("auth config", "err", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backs, cleanupBackends, err := backend.BuildUpstreams(rootCtx, cfg.Backends)
	if err != nil {
		slog.Error("backends", "err", err)
		os.Exit(1)
	}
	defer cleanupBackends()

	aggOpts, err := aggregatorOptions(cfg)
	if err != nil {
		slog.Error("aggregator", "err", err)
		os.Exit(1)
	}
	agg, err := aggregate.New(backs, aggOpts...)
	if err != nil {
		slog.Error("aggregate", "err", err)
		os.Exit(1)
	}

	httpOpts := orchestrator.HTTPMiddlewareOptions("mcp-gateway", authCfg, validator)
	httpOpts = append(httpOpts, httpserver.WithShutdownContext(rootCtx))
	srv := httpserver.New(agg, addr, httpOpts...)

	go func() {
		slog.Info("mcp-gateway listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			stop()
		}
	}()

	<-rootCtx.Done()
	slog.Info("shutdown signal received")

	sctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		slog.Error("http shutdown", "err", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
