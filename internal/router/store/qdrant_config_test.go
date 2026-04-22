package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewQdrantValidation(t *testing.T) {
	_, err := NewQdrant("", "c", 384)
	require.Error(t, err)
	_, err = NewQdrant("http://localhost:6333", "", 384)
	require.Error(t, err)
	_, err = NewQdrant("http://localhost:6333", "x", 0)
	require.Error(t, err)
	q, err := NewQdrant("http://localhost:6333", "x", 384)
	require.NoError(t, err)
	require.NotNil(t, q)
}
