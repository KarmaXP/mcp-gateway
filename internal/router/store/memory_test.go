package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInMemoryVectorStoreUpsertQueryFilter(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryVectorStore(3)
	require.NoError(t, m.Upsert(ctx, []ToolVectorRecord{
		{ID: "1", Vector: []float32{1, 0, 0}, ToolName: "a__x", UpstreamID: "b", CatalogVersion: "v1"},
		{ID: "2", Vector: []float32{0, 1, 0}, ToolName: "a__y", UpstreamID: "b", CatalogVersion: "v1"},
	}))
	res, err := m.Query(ctx, []float32{1, 0, 0}, 2, VectorSearchFilter{CatalogVersion: "v1", AllowedToolNames: []string{"a__x"}})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "a__x", res[0].ToolName)
}

func TestInMemoryVectorStoreDimensionMismatch(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryVectorStore(2)
	err := m.Upsert(ctx, []ToolVectorRecord{{ID: "1", Vector: []float32{1, 0, 0}, ToolName: "t", CatalogVersion: "v"}})
	require.ErrorIs(t, err, ErrDimensionMismatch)
	_, err = m.Query(ctx, []float32{1}, 4, VectorSearchFilter{})
	require.ErrorIs(t, err, ErrDimensionMismatch)
}

func TestL2Normalize(t *testing.T) {
	v := []float32{3, 4, 0}
	require.True(t, L2Normalize(v))
	require.InDelta(t, 0.6, float64(v[0]), 1e-6)
	require.InDelta(t, 0.8, float64(v[1]), 1e-6)
}

func TestCosineViaQuery(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryVectorStore(2)
	_ = m.Upsert(ctx, []ToolVectorRecord{
		{ID: "1", Vector: []float32{1, 0}, ToolName: "t1", CatalogVersion: "v"},
	})
	out, err := m.Query(ctx, []float32{1, 0}, 4, VectorSearchFilter{CatalogVersion: "v"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.InDelta(t, 1.0, out[0].Score, 1e-6)
}

func TestInMemoryVectorStoreQueryRequiresCatalogVersion(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryVectorStore(2)
	require.NoError(t, m.Upsert(ctx, []ToolVectorRecord{
		{ID: "1", Vector: []float32{1, 0}, ToolName: "t1", CatalogVersion: "v"},
	}))
	out, err := m.Query(ctx, []float32{1, 0}, 4, VectorSearchFilter{})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestInMemoryVectorStoreQueryEmptyAllowedBlocksAll(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryVectorStore(2)
	require.NoError(t, m.Upsert(ctx, []ToolVectorRecord{
		{ID: "1", Vector: []float32{1, 0}, ToolName: "t1", CatalogVersion: "v"},
	}))
	out, err := m.Query(ctx, []float32{1, 0}, 4, VectorSearchFilter{
		CatalogVersion:   "v",
		AllowedToolNames: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestInMemoryVectorStoreDeleteCatalogVersion(t *testing.T) {
	ctx := context.Background()
	m := NewInMemoryVectorStore(3)
	require.NoError(t, m.Upsert(ctx, []ToolVectorRecord{
		{ID: "a", Vector: []float32{1, 0, 0}, ToolName: "t1", UpstreamID: "b", CatalogVersion: "v1"},
		{ID: "b", Vector: []float32{0, 1, 0}, ToolName: "t2", UpstreamID: "b", CatalogVersion: "v2"},
	}))
	require.NoError(t, m.DeleteCatalogVersion(ctx, "v1"))
	res, err := m.Query(ctx, []float32{0, 1, 0}, 4, VectorSearchFilter{CatalogVersion: "v2"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "t2", res[0].ToolName)
	empty, err := m.Query(ctx, []float32{1, 0, 0}, 4, VectorSearchFilter{CatalogVersion: "v1"})
	require.NoError(t, err)
	require.Len(t, empty, 0)
}
