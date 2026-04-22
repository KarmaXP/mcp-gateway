package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitShutdownWithoutOTLPEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "mcp-gateway-test")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 5)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}
