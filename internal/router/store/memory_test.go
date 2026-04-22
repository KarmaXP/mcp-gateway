package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryUpsertQueryFilter(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(3)
	require.NoError(t, m.Upsert(ctx, []Point{
		{ID: "1", Vector: []float32{1, 0, 0}, ToolName: "a__x", Backend: "b", Version: "v1"},
		{ID: "2", Vector: []float32{0, 1, 0}, ToolName: "a__y", Backend: "b", Version: "v1"},
	}))
	res, err := m.Query(ctx, []float32{1, 0, 0}, 2, Filter{CatalogVersion: "v1", AllowedTools: []string{"a__x"}})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "a__x", res[0].ToolName)
}

func TestMemoryDimensionMismatch(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(2)
	err := m.Upsert(ctx, []Point{{ID: "1", Vector: []float32{1, 0, 0}, ToolName: "t", Version: "v"}})
	require.ErrorIs(t, err, ErrDimensionMismatch)
	_, err = m.Query(ctx, []float32{1}, 4, Filter{})
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
	m := NewMemory(2)
	_ = m.Upsert(ctx, []Point{
		{ID: "1", Vector: []float32{1, 0}, ToolName: "t1", Version: "v"},
	})
	out, err := m.Query(ctx, []float32{1, 0}, 4, Filter{})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.InDelta(t, 1.0, out[0].Score, 1e-6)
}

func TestMemoryDeleteCatalogVersion(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(3)
	require.NoError(t, m.Upsert(ctx, []Point{
		{ID: "a", Vector: []float32{1, 0, 0}, ToolName: "t1", Backend: "b", Version: "v1"},
		{ID: "b", Vector: []float32{0, 1, 0}, ToolName: "t2", Backend: "b", Version: "v2"},
	}))
	require.NoError(t, m.DeleteCatalogVersion(ctx, "v1"))
	res, err := m.Query(ctx, []float32{0, 1, 0}, 4, Filter{CatalogVersion: "v2"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "t2", res[0].ToolName)
	empty, err := m.Query(ctx, []float32{1, 0, 0}, 4, Filter{CatalogVersion: "v1"})
	require.NoError(t, err)
	require.Len(t, empty, 0)
}
