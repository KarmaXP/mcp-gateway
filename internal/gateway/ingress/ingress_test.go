package ingress

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithMCPIntentRoundTrip(t *testing.T) {
	ctx := WithMCPIntent(context.Background(), "  list pods  ")
	require.Equal(t, "list pods", MCPIntentFromContext(ctx))
	require.Equal(t, "", MCPIntentFromContext(context.Background()))
}

func TestWithAllowedToolsRoundTrip(t *testing.T) {
	ctx := WithAllowedTools(context.Background(), []string{"k8s__logs", "  prom__q  "})
	got := AllowedToolsFromContext(ctx)
	require.Equal(t, []string{"k8s__logs", "prom__q"}, got)
	require.Nil(t, AllowedToolsFromContext(context.Background()))
}
