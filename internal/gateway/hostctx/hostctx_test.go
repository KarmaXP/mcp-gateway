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

func TestMergeRequestValuesCopiesHostScopedFields(t *testing.T) {
	req := WithPolicyVersion(
		WithSubjectID(
			WithAllowedToolNames(
				WithClientIntent(context.Background(), "list pods"),
				[]string{"p__echo"},
			),
			"user-1",
		),
		"v3",
	)
	parent := context.Background()
	merged := MergeRequestValues(parent, req)
	require.Equal(t, "list pods", ClientIntentFromContext(merged))
	require.Equal(t, []string{"p__echo"}, AllowedToolNamesFromContext(merged))
	require.Equal(t, "user-1", SubjectIDFromContext(merged))
	require.Equal(t, "v3", PolicyVersionFromContext(merged))
	require.Equal(t, "", ClientIntentFromContext(parent))
}
