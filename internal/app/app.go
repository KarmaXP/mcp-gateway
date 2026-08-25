package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/auth/ratelimit"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/orchestrator"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type Options struct {
	ServiceName string
	Config      config.GatewayConfig
}

type App struct {
	srv     *httpserver.Server
	policy  *policy.Holder
	addr    string
	closers []func()
}

func New(ctx context.Context, opts Options) (app *App, err error) {
	a := &App{addr: resolveListenAddr(os.Getenv)}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	auditor, auditCleanup, err := configureAuditSink(opts.Config, nil, os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("policy audit sink: %w", err)
	}
	if auditCleanup != nil {
		a.closers = append(a.closers, auditCleanup)
	}

	if err := preflightQdrant(ctx, opts.Config, os.Getenv); err != nil {
		return nil, fmt.Errorf("preflight: %w", err)
	}

	authCfg := auth.JWTAuthFromEnvironment()
	validator, err := auth.NewValidator(authCfg)
	if err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}

	engineInput, err := policyEngineInput(opts.Config)
	if err != nil {
		return nil, err
	}
	a.policy = policy.NewHolder(policy.NewEngine(engineInput))

	upstreams, cleanupUpstreams, err := connectUpstreams(ctx, opts.Config.Upstreams)
	if err != nil {
		return nil, fmt.Errorf("connect upstreams: %w", err)
	}
	a.closers = append(a.closers, cleanupUpstreams)

	mpxOpts, err := multiplexOptions(opts.Config, os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("multiplexer options: %w", err)
	}
	mpxOpts = append(mpxOpts,
		multiplex.WithPolicyHolder(a.policy),
		multiplex.WithAuditor(auditor),
		multiplex.WithArgumentValidateLimits(argumentLimits(opts.Config)),
		multiplex.WithAggregationStrict(opts.Config.Aggregation.StrictInitialize, opts.Config.Aggregation.StrictList),
		multiplex.WithReportPartialFailures(opts.Config.Aggregation.ReportPartialFailures),
	)
	mpx, err := multiplex.New(ctx, upstreams, mpxOpts...)
	if err != nil {
		return nil, fmt.Errorf("multiplexer: %w", err)
	}

	httpOpts := []httpserver.Option{httpserver.WithOriginAllowList(opts.Config.AllowedOrigins())}
	if checker := newReadinessChecker(opts.Config, os.Getenv, http.DefaultClient); checker != nil {
		httpOpts = append(httpOpts, httpserver.WithReadinessChecker(checker))
	}
	limiter := ratelimit.New(ctx, rateLimitConfig(opts.Config))
	httpOpts = append(httpOpts, orchestrator.HTTPServerOptions(serviceName(opts), authCfg, validator, a.policy, limiter)...)
	httpOpts = append(httpOpts, httpserver.WithShutdownContext(ctx))
	a.srv = httpserver.New(mpx, a.addr, httpOpts...)

	if opts.Config.ForwardToolsListChanged() {
		backend.RegisterNotificationHandlers(upstreams, func(req *rpc.Request) {
			if req == nil || !mcpwire.IsCatalogListChangedNotification(req.Method) {
				return
			}
			if mcpwire.IsToolsListChangedNotification(req.Method) {
				mpx.HandleToolsListChanged()
			}
			a.srv.BroadcastNotification(req)
		})
		slog.Info("upstream catalog list_changed forwarding enabled")
	}
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("mcp-gateway listening", "addr", a.addr)
		if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	reloadDone := a.watchSIGHUP(runCtx)

	var runErr error
	select {
	case err := <-serveErr:
		runErr = err
	case <-runCtx.Done():
		slog.Info("shutdown signal received")
	}
	stop()
	<-reloadDone

	sctx, cancel := context.WithTimeout(context.Background(), defaults.HTTPServerShutdownTimeout)
	defer cancel()
	if err := a.srv.Shutdown(sctx); err != nil {
		return errors.Join(runErr, fmt.Errorf("http shutdown: %w", err))
	}
	slog.Info("shutdown complete")
	return runErr
}

func (a *App) Close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
	a.closers = nil
}

func serviceName(opts Options) string {
	if opts.ServiceName == "" {
		return defaults.DefaultTelemetryServiceName
	}
	return opts.ServiceName
}

func (a *App) watchSIGHUP(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer close(done)
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				cfg, err := config.Load()
				if err != nil {
					slog.Error("policy reload skipped: config load failed", "err", err)
					continue
				}
				reloaded, err := policyEngineInput(cfg)
				if err != nil {
					slog.Error("policy reload skipped: invalid tool pattern", "err", err)
					continue
				}
				policy.ReloadEngine(a.policy, reloaded)
				slog.Warn("SIGHUP applies policy-only reload", "reloaded", "config.Load + policy.ReloadEngine", "not_reloaded", "rate_limit, allowed_origins, aggregation, audit_sink, backends, max_in_flight")
				slog.Info("policy reloaded from config", "policy_version", cfg.Policy.Version)
			}
		}
	}()
	return done
}
