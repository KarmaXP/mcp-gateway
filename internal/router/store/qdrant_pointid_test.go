package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPointIDStableAndUnique(t *testing.T) {
	a := pointID("v1::a__x")
	b := pointID("v1::a__y")
	require.Equal(t, a, pointID("v1::a__x"))
	require.NotEqual(t, a, b)
	require.Len(t, a, 36)
	require.Equal(t, '-', rune(a[8]))
}
