package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

const (
	readinessProbeTimeout = 2 * time.Second
	probeBodyDrainLimit   int64 = 4 << 10
)

type dependencyReadinessChecker struct {
	httpClient *http.Client
	qdrantURL  string
	embedURL   string
}

func (c *dependencyReadinessChecker) CheckReadiness(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := probeAnyHealthPath(gctx, c.httpClient, c.qdrantURL, "/readyz", "/healthz"); err != nil {
			return fmt.Errorf("qdrant dependency unhealthy: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		if err := probeAnyHealthPath(gctx, c.httpClient, c.embedURL, "/healthz"); err != nil {
			return fmt.Errorf("embed dependency unhealthy: %w", err)
		}
		return nil
	})
	return g.Wait()
}

func probeAnyHealthPath(ctx context.Context, client *http.Client, baseURL string, paths ...string) error {
	var lastErr error
	for _, path := range paths {
		err := probeHealthPath(ctx, client, baseURL, path)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
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
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, probeBodyDrainLimit))
		_ = res.Body.Close()
	}()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("probe %s returned status %d", u, res.StatusCode)
	}
	return nil
}

func routerModeActive(cfg config.GatewayConfig) bool {
	mode, _ := mode.Parse(cfg.SemanticRouter.Mode)
	return mode.Active()
}

func newReadinessChecker(cfg config.GatewayConfig, getenv func(string) string, client *http.Client) httpserver.ReadinessChecker {
	if !routerModeActive(cfg) {
		return nil
	}
	qdrantURL := strings.TrimSpace(getenv("QDRANT_URL"))
	embedURL := strings.TrimSpace(cfg.Embedding.URL)
	if embedURL == "" {
		embedURL = defaults.DefaultEmbedServiceURL
	}
	return &dependencyReadinessChecker{
		httpClient: client,
		qdrantURL:  qdrantURL,
		embedURL:   embedURL,
	}
}

func preflightStrictEnabled(getenv func(string) string) bool {
	v := strings.ToLower(strings.TrimSpace(getenv("GATEWAY_PREFLIGHT_STRICT")))
	return v == "1" || v == "true" || v == "yes"
}

func preflightQdrant(ctx context.Context, cfg config.GatewayConfig, getenv func(string) string) error {
	if !routerModeActive(cfg) {
		return nil
	}
	qURL := strings.TrimSpace(getenv("QDRANT_URL"))
	if qURL == "" {
		return nil
	}
	pctx, cancel := context.WithTimeout(ctx, defaults.PreflightQdrantTimeout)
	defer cancel()
	if err := store.PingCollections(pctx, qURL); err != nil {
		if preflightStrictEnabled(getenv) {
			return fmt.Errorf("qdrant preflight failed (url=%s): %w", qURL, err)
		}
		slog.Warn("qdrant preflight failed (continuing; router/index may fail until Qdrant is healthy)",
			"err", err,
			"url", qURL,
		)
		return nil
	}
	slog.Info("qdrant preflight ok", "url", qURL)
	return nil
}
