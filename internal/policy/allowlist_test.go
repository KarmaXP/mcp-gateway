package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllowedListContains_ExactAndGlob(t *testing.T) {
	allowed := []string{"alpha__exact", "beta__*"}
	ok, err := AllowedListContains("alpha__exact", allowed)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = AllowedListContains("beta__tool", allowed)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = AllowedListContains("gamma__x", allowed)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestAllowedListContains_EmptyMeansOpen(t *testing.T) {
	ok, err := AllowedListContains("any__tool", nil)
	require.NoError(t, err)
	require.True(t, ok)
}
