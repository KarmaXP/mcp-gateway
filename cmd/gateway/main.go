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
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/orchestrator"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

type auditSinkCloser interface {
	policy.AuditSink
	Close() error
}

var newSyslogAuditSink = func(network, address string) (auditSinkCloser, error) {
	return policy.NewSyslogAuditSink(network, address)
}

const readinessProbeTimeout = 2 * time.Second

type dependencyReadinessChecker struct {
	httpClient *http.Client
	qdrantURL  string
	embedURL   string
}

func (c *dependencyReadinessChecker) CheckReadiness(ctx context.Context) error {
	if err := probeAnyHealthPath(ctx, c.httpClient, c.qdrantURL, "/readyz", "/healthz"); err != nil {
		return fmt.Errorf("qdrant dependency unhealthy: %w", err)
	}
	if err := probeAnyHealthPath(ctx, c.httpClient, c.embedURL, "/healthz"); err != nil {
		return fmt.Errorf("embed dependency unhealthy: %w", err)
	}
	return nil
}

func probeAnyHealthPath(ctx context.Context, client *http.Client, baseURL string, paths ...string) error {
	var lastErr error
	for _, path := range paths {
		if err := probeHealthPath(ctx, client, baseURL, path); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no readiness probe paths configured for %s", baseURL)
}

func probeHealthPath(ctx context.Context, client *http.Client, baseURL, path string) error {
	if strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("missing dependency base URL for path %s", path)
	}
	probeCtx, cancel := context.WithTimeout(ctx, readinessProbeTimeout)
	defer cancel()
	u := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request %s: %w", u, err)
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s failed: %w", u, err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("probe %s returned status %d", u, res.StatusCode)
	}
	return nil
}

func routerModeActive(cfg config.GatewayConfig) bool {
	mode := strings.ToLower(strings.TrimSpace(cfg.SemanticRouter.Mode))
	return mode == "on" || mode == "assist_list" || mode == "filter_list"
}

func buildReadinessChecker(cfg config.GatewayConfig) httpserver.ReadinessChecker {
	if !routerModeActive(cfg) {
		return nil
	}
	qdrantURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	embedURL := strings.TrimSpace(cfg.Embedding.URL)
	if embedURL == "" {
		embedURL = defaults.DefaultEmbedServiceURL
	}
	return &dependencyReadinessChecker{
		httpClient: http.DefaultClient,
		qdrantURL:  qdrantURL,
		embedURL:   embedURL,
	}
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
		multiplex.WithListTTL(cfg.AggregationListCacheTTL()),
		multiplex.WithInitTimeout(cfg.AggregationInitTimeout()),
		multiplex.WithListTimeout(cfg.AggregationListTimeout()),
		multiplex.WithCallTimeout(cfg.AggregationCallTimeout()),
		multiplex.WithGlobalMaxInFlight(cfg.AggregationMaxInFlight()),
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

func configureAuditSink(cfg config.GatewayConfig) (func(), error) {
	auditCfg, err := cfg.ResolvePolicyAuditSink()
	if err != nil {
		return nil, err
	}
	switch auditCfg.SinkType {
	case config.PolicyAuditSinkSlog:
		policy.SetAuditSink(policy.SlogAuditSink{})
		slog.Info("policy audit sink configured", "sink", config.PolicyAuditSinkSlog)
		return nil, nil
	case config.PolicyAuditSinkSyslog:
		sink, err := newSyslogAuditSink(auditCfg.SyslogNetwork, auditCfg.SyslogAddress)
		if err != nil {
			return nil, err
		}
		policy.SetAuditSink(sink)
		slog.Info("policy audit sink configured",
			"sink", config.PolicyAuditSinkSyslog,
			"network", auditCfg.SyslogNetwork,
			"address", auditCfg.SyslogAddress,
		)
		return func() {
			if err := sink.Close(); err != nil {
				slog.Warn("policy audit sink close", "err", err)
			}
		}, nil
	default:
		return nil, fmt.Errorf("unsupported audit sink type %q", auditCfg.SinkType)
	}
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
	auditCleanup, err := configureAuditSink(cfg)
	if err != nil {
		slog.Error("policy audit sink", "err", err)
		os.Exit(1)
	}
	if auditCleanup != nil {
		defer auditCleanup()
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
	mpxOpts = append(mpxOpts, multiplex.WithArgumentValidateLimits(cfg.PolicyArgumentLimits()))
	mpxOpts = append(mpxOpts, multiplex.WithAggregationStrict(cfg.Aggregation.StrictInitialize, cfg.Aggregation.StrictList))
	mpxOpts = append(mpxOpts, multiplex.WithReportPartialFailures(cfg.Aggregation.ReportPartialFailures))
	mpx, err := multiplex.New(upstreams, mpxOpts...)
	if err != nil {
		slog.Error("multiplexer", "err", err)
		os.Exit(1)
	}

	httpOpts := []httpserver.Option{
		httpserver.WithOriginAllowList(cfg.AllowedOrigins()),
	}
	if readinessChecker := buildReadinessChecker(cfg); readinessChecker != nil {
		httpOpts = append(httpOpts, httpserver.WithReadinessChecker(readinessChecker))
	}
	httpOpts = append(httpOpts, orchestrator.HTTPServerOptions(defaults.DefaultTelemetryServiceName, authCfg, validator, polHolder, cfg.RateLimit())...)
	httpOpts = append(httpOpts, httpserver.WithShutdownContext(rootCtx))
	srv := httpserver.New(mpx, addr, httpOpts...)

	if cfg.ForwardToolsListChanged() {
		backend.RegisterNotificationHandlers(upstreams, func(req *rpc.Request) {
			if req == nil || !mcpwire.IsCatalogListChangedNotification(req.Method) {
				return
			}
			if mcpwire.IsToolsListChangedNotification(req.Method) {
				mpx.HandleToolsListChanged(context.Background())
			}
			srv.BroadcastNotification(req)
		})
		slog.Info("upstream catalog list_changed forwarding enabled")
	}

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
			slog.Warn("SIGHUP applies policy-only reload", "reloaded", "config.Load + policy.ReloadEngine", "not_reloaded", "rate_limit, allowed_origins, aggregation, audit_sink, backends, max_in_flight")
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
