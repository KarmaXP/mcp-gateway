//go:build integration

package telemetry

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInitShutdownWithLiveOTLPCollector(t *testing.T) {
	ep := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if ep == "" {
		ep = "http://127.0.0.1:4318"
	}
	if !probeHealth(context.Background(), ep) {
		t.Skip("OTLP collector not reachable at ", ep, " — start compose (otel-collector) or set OTEL_EXPORTER_OTLP_ENDPOINT; not required in default CI integration job")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", ep)

	shutdown, err := Init(context.Background(), "mcp-gateway-otlp-integration")
	require.NoError(t, err)

	tctx, span := StartSpan(context.Background(), "integration.otlp.span")
	span.End()
	_ = tctx

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}

func probeHealth(ctx context.Context, ep string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(ep, "/"), nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode < 500
}
