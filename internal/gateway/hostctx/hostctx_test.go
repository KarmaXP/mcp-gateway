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

func TestWithAllowListRoundTrip(t *testing.T) {
	ctx := WithAllowList(context.Background(), []string{"k8s__logs", "  prom__q  "})
	_, got := AllowListModeFromContext(ctx)
	require.Equal(t, []string{"k8s__logs", "prom__q"}, got)
	mode, _ := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListRestricted, mode)
	_, unrestricted := AllowListModeFromContext(context.Background())
	require.Nil(t, unrestricted)
}

func TestWithAllowListPreservesEmptySliceDenyAll(t *testing.T) {
	ctx := WithAllowList(context.Background(), []string{})
	_, got := AllowListModeFromContext(ctx)
	require.NotNil(t, got)
	require.Empty(t, got)
	mode, names := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListDenyAll, mode)
	require.Empty(t, names)
}

func TestWithAllowListNilMeansUnrestricted(t *testing.T) {
	ctx := WithAllowList(context.Background(), nil)
	mode, names := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListUnrestricted, mode)
	require.Nil(t, names)
}

func TestMergeRequestValuesClearsIntentOnEmptyPOST(t *testing.T) {
	parent := WithClientIntent(context.Background(), "old intent")
	req := WithClientIntent(context.Background(), "")
	merged := MergeRequestValues(parent, req)
	require.Equal(t, "", ClientIntentFromContext(merged))
}

func TestMergeRequestValuesCopiesDenyAllAllowList(t *testing.T) {
	req := WithAllowList(context.Background(), []string{})
	merged := MergeRequestValues(context.Background(), req)
	mode, _ := AllowListModeFromContext(merged)
	require.Equal(t, AllowListDenyAll, mode)
}

func TestMergeRequestValuesCopiesHostScopedFields(t *testing.T) {
	req := WithPolicyVersion(
		WithSubjectID(
			WithAllowList(
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
	_, mergedNames := AllowListModeFromContext(merged)
	require.Equal(t, []string{"p__echo"}, mergedNames)
	require.Equal(t, "user-1", SubjectIDFromContext(merged))
	require.Equal(t, "v3", PolicyVersionFromContext(merged))
	require.Equal(t, "", ClientIntentFromContext(parent))
}
