package hostctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithClientIntentRoundTrip(t *testing.T) {
	ctx := WithClientIntent(context.Background(), "  list pods  ")
	require.Equal(t, "list pods", ClientIntentFromContext(ctx))
	require.Equal(t, "", ClientIntentFromContext(context.Background()))
}

func TestWithAllowedToolNamesRoundTrip(t *testing.T) {
	ctx := WithAllowedToolNames(context.Background(), []string{"k8s__logs", "  prom__q  "})
	got := AllowedToolNamesFromContext(ctx)
	require.Equal(t, []string{"k8s__logs", "prom__q"}, got)
	require.Nil(t, AllowedToolNamesFromContext(context.Background()))
}
