package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/orchestrator"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

// aggregatorOptions returns base options plus optional semantic router (plan §3.B).
// Enable with ROUTER_MODE=on (or assist_list). EMBED_URL defaults to http://127.0.0.1:8001.
func aggregatorOptions() []aggregate.Option {
	opts := []aggregate.Option{aggregate.WithListTTL(0)}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("ROUTER_MODE")))
	if mode != "on" && mode != "assist_list" {
		return opts
	}

	embedURL := strings.TrimSpace(os.Getenv("EMBED_URL"))
	if embedURL == "" {
		embedURL = "http://127.0.0.1:8001"
	}
	dim := 384
	if v := os.Getenv("ROUTER_VECTOR_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			dim = n
		}
	}

	cfg := router.DefaultConfig()
	cfg.Mode = router.ModeAssistList
	if v := os.Getenv("ROUTER_SCORE_MIN"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.ScoreMin = f
		}
	}
	if v := os.Getenv("ROUTER_TOP_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.TopK = n
		}
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("ROUTER_ALLOW_AUTO_RENAME"))); v == "1" || v == "true" || v == "yes" {
		cfg.AllowAutoRename = true
	}

	e := router.NewEngine(cfg, embed.NewClient(embedURL), store.NewMemory(dim), dim)
	opts = append(opts, aggregate.WithSemanticRouter(e))
	slog.Info("semantic router enabled",
		"embed_url", embedURL,
		"vector_dim", dim,
		"score_min", cfg.ScoreMin,
		"top_k", cfg.TopK,
		"allow_auto_rename", cfg.AllowAutoRename,
	)
	return opts
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

	// Phase 1: two mock backends with distinct prefixes (§A.7 acceptance criteria).
	b1 := mock.New("backend-alpha", "alpha", []string{"echo", "list"})
	b2 := mock.New("backend-beta", "beta", []string{"ping"})

	agg, err := aggregate.New(
		[]backend.Backend{b1, b2},
		aggregatorOptions()...,
	)
	if err != nil {
		slog.Error("aggregate", "err", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
