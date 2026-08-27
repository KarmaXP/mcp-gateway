package hostctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnknownAllowListModeFromContextDenies(t *testing.T) {
	ctx := context.WithValue(context.Background(), allowedToolNamesKey{}, allowListState{
		mode:  AllowListMode(99),
		names: []string{"alpha__echo"},
	})

	mode, names := AllowListModeFromContext(ctx)

	require.Equal(t, AllowListDenyAll, mode,
		"a mode this function does not know must deny, or adding one widens access silently")
	require.Empty(t, names)
}

func TestUnknownAllowListModeShowsNoToolsToPolicy(t *testing.T) {
	require.Equal(t, []string{}, PolicyAllowListView(AllowListMode(99), []string{"alpha__echo"}),
		"an unknown mode must present the deny-all view, not the caller's names")
}
