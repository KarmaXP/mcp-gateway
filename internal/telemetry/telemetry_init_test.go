package telemetry

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInitShutdownWithoutOTLPEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "mcp-gateway-test")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}

func TestInitWithOTLPEndpointDoesNotFailWhenCollectorUnreachable(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:9")
	shutdown, err := Init(context.Background(), "mcp-gateway-otlp-test")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	tctx, span := StartSpan(context.Background(), "smoke.export.path")
	span.End()
	_ = tctx

	ctx2, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := shutdown(ctx2); err != nil {
		require.Contains(t, err.Error(), "127.0.0.1:9")
	}
}

func TestInitImplicitHTTPSchemeForBareHost(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:9")
	shutdown, err := Init(context.Background(), "mcp-gateway-bare-endpoint")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		require.Contains(t, err.Error(), "127.0.0.1:9")
	}
}
