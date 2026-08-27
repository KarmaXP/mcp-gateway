package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllowedListContains_ExactAndGlob(t *testing.T) {
	allowed := []string{"alpha__exact", "beta__*"}
	ok, err := AllowListPermits("alpha__exact", allowed)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = AllowListPermits("beta__tool", allowed)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = AllowListPermits("gamma__x", allowed)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAllowedListContains_NilMeansOpen(t *testing.T) {
	ok, err := AllowListPermits("any__tool", nil)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAllowedListContains_EmptySliceDenyAll(t *testing.T) {
	ok, err := AllowListPermits("any__tool", []string{})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAllowedListContains_StarEntryIsTheFullCatalog(t *testing.T) {
	for _, tool := range []string{"alpha__echo", "k8s__get_pod_logs", "x"} {
		ok, err := AllowListPermits(tool, []string{"*"})
		require.NoError(t, err)
		require.True(t, ok, "a single * entry is how a principal asks for the whole catalog")
	}
}
