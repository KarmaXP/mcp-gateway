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
	mode, _ := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListRestricted, mode)
	require.Nil(t, AllowedToolNamesFromContext(context.Background()))
}

func TestWithAllowedToolNamesPreservesEmptySliceDenyAll(t *testing.T) {
	ctx := WithAllowedToolNames(context.Background(), []string{})
	got := AllowedToolNamesFromContext(ctx)
	require.NotNil(t, got)
	require.Empty(t, got)
	mode, names := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListDenyAll, mode)
	require.Empty(t, names)
}

func TestWithAllowedToolNamesNilMeansUnrestricted(t *testing.T) {
	ctx := WithAllowedToolNames(context.Background(), nil)
	mode, names := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListUnrestricted, mode)
	require.Nil(t, names)
	require.Nil(t, AllowedToolNamesFromContext(ctx))
}

func TestAttachPolicyAllowListExplicitUnrestricted(t *testing.T) {
	ctx := AttachPolicyAllowList(context.Background(), nil)
	mode, names := AllowListModeFromContext(ctx)
	require.Equal(t, AllowListUnrestricted, mode)
	require.Nil(t, names)
}

func TestMergeRequestValuesClearsAllowListOnUnrestrictedPOST(t *testing.T) {
	parent := WithAllowedToolNames(context.Background(), []string{"a__x"})
	req := AttachPolicyAllowList(context.Background(), nil)
	merged := MergeRequestValues(parent, req)
	mode, names := AllowListModeFromContext(merged)
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
	req := WithAllowedToolNames(context.Background(), []string{})
	merged := MergeRequestValues(context.Background(), req)
	mode, _ := AllowListModeFromContext(merged)
	require.Equal(t, AllowListDenyAll, mode)
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
