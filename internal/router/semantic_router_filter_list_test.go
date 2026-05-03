package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func TestFilterToolsForListEmptyIntentReturnsFull(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = ModeFilterList
	sr := NewSemanticRouter(cfg, emb, st, dim)
	row := index.ToolRow{Name: "pre__tool", Description: "d", ParamKeys: nil}
	require.NoError(t, sr.Reindex(context.Background(), "v1", []IndexedTool{
		{ToolRow: row, UpstreamID: "b1"},
	}))
	keep, full := sr.FilterToolsForList(context.Background(), RoutingSignal{IntentText: "   "})
	require.Nil(t, keep)
	require.True(t, full)
}

func TestFilterToolsForListSubsetsByIntent(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: []string{"x"}}
	t2 := index.ToolRow{Name: "a__two", Description: "second", ParamKeys: nil}
	d1 := index.FormatDocument(t1)
	d2 := index.FormatDocument(t2)
	emb.vecs[d1] = []float32{1, 0, 0, 0}
	emb.vecs[d2] = []float32{0, 1, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = ModeFilterList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	sr := NewSemanticRouter(cfg, emb, st, dim)
	require.NoError(t, sr.Reindex(context.Background(), "v1", []IndexedTool{
		{ToolRow: t1, UpstreamID: "b1"},
		{ToolRow: t2, UpstreamID: "b1"},
	}))

	q := index.FormatQuery("", "match first", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	keep, full := sr.FilterToolsForList(context.Background(), RoutingSignal{
		Method:         "tools/list",
		IntentText:     "match first",
		CatalogVersion: "v1",
	})
	require.False(t, full)
	require.Contains(t, keep, "a__one")
	require.NotContains(t, keep, "a__two")
}

func TestFilterToolsForListStaleCatalogReturnsFull(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = ModeFilterList
	sr := NewSemanticRouter(cfg, emb, st, dim)
	row := index.ToolRow{Name: "pre__tool", Description: "d", ParamKeys: nil}
	require.NoError(t, sr.Reindex(context.Background(), "v1", []IndexedTool{
		{ToolRow: row, UpstreamID: "b1"},
	}))
	keep, full := sr.FilterToolsForList(context.Background(), RoutingSignal{
		IntentText:     "any",
		CatalogVersion: "stale-not-v1",
	})
	require.Nil(t, keep)
	require.True(t, full)
}

func TestFilterToolsForListScoreMinExcludesAllReturnsFull(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: nil}
	t2 := index.ToolRow{Name: "a__two", Description: "second", ParamKeys: nil}
	d1 := index.FormatDocument(t1)
	d2 := index.FormatDocument(t2)
	emb.vecs[d1] = []float32{1, 0, 0, 0}
	emb.vecs[d2] = []float32{0, 1, 0, 0}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = ModeFilterList
	cfg.TopK = 4
	cfg.ScoreMin = 1.01
	sr := NewSemanticRouter(cfg, emb, st, dim)
	require.NoError(t, sr.Reindex(context.Background(), "v1", []IndexedTool{
		{ToolRow: t1, UpstreamID: "b1"},
		{ToolRow: t2, UpstreamID: "b1"},
	}))
	q := index.FormatQuery("", "match first", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}
	keep, full := sr.FilterToolsForList(context.Background(), RoutingSignal{
		IntentText:     "match first",
		CatalogVersion: "v1",
	})
	require.Nil(t, keep)
	require.True(t, full)
}

func TestFilterToolsForListAllowedToolsRestrictsHits(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: nil}
	t2 := index.ToolRow{Name: "a__two", Description: "second", ParamKeys: nil}
	d1 := index.FormatDocument(t1)
	d2 := index.FormatDocument(t2)
	emb.vecs[d1] = []float32{1, 0, 0, 0}
	emb.vecs[d2] = []float32{0, 1, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = ModeFilterList
	cfg.TopK = 4
	cfg.ScoreMin = 0.5
	sr := NewSemanticRouter(cfg, emb, st, dim)
	require.NoError(t, sr.Reindex(context.Background(), "v1", []IndexedTool{
		{ToolRow: t1, UpstreamID: "b1"},
		{ToolRow: t2, UpstreamID: "b1"},
	}))

	q := index.FormatQuery("", "match first", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	keep, full := sr.FilterToolsForList(context.Background(), RoutingSignal{
		Method:         "tools/list",
		IntentText:     "match first",
		AllowedTools:   []string{"a__one"},
		CatalogVersion: "v1",
	})
	require.False(t, full)
	require.Contains(t, keep, "a__one")
	require.NotContains(t, keep, "a__two")
}
