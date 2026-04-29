package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewQdrantVectorStoreValidation(t *testing.T) {
	_, err := NewQdrantVectorStore("", "c", 384)
	require.Error(t, err)
	_, err = NewQdrantVectorStore("http://localhost:6333", "", 384)
	require.Error(t, err)
	_, err = NewQdrantVectorStore("http://localhost:6333", "x", 0)
	require.Error(t, err)
	q, err := NewQdrantVectorStore("http://localhost:6333", "x", 384)
	require.NoError(t, err)
	require.NotNil(t, q)
}
