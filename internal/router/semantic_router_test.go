package router

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

type mapEmbed struct {
	vecs map[string][]float32
	dim  int
}

func (m *mapEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := m.vecs[t]
		if len(v) == 0 {
			v = make([]float32, m.dim)
			v[0] = 0.01
		}
		cp := append([]float32(nil), v...)
		store.L2Normalize(cp)
		out[i] = cp
	}
	return out, nil
}

func TestSemanticRouterExactShortcutNoVector(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.9
	e := NewSemanticRouter(cfg, emb, st, dim)

	row := index.Tool{Name: "pre__tool", Description: "d", ParamKeys: nil}
	doc := index.FormatDocument(row)
	emb.vecs[doc] = []float32{1, 0, 0, 0}

	reindexAndApply(t, e, context.Background(), "v1", []CatalogEntry{
		{Tool: row, UpstreamID: "be1"},
	})

	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "pre__tool"})
	require.NoError(t, err)
	require.Equal(t, "pre__tool", name)
	require.Equal(t, "exact", dec.FallbackLayer)
	require.Equal(t, OutcomeExact, dec.Outcome)
}

func TestSemanticRouterVectorResolvesWhenNameWrong(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	t1 := index.Tool{Name: "a__one", Description: "first", ParamKeys: []string{"x"}}
	t2 := index.Tool{Name: "a__two", Description: "second", ParamKeys: nil}
	d1 := index.FormatDocument(t1)
	d2 := index.FormatDocument(t2)
	emb.vecs[d1] = []float32{1, 0, 0, 0}
	emb.vecs[d2] = []float32{0, 1, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	cfg.AllowAutoRename = true
	e := NewSemanticRouter(cfg, emb, st, dim)

	reindexAndApply(t, e, context.Background(), "v1", []CatalogEntry{
		{Tool: t1, UpstreamID: "b1"},
		{Tool: t2, UpstreamID: "b1"},
	})

	q := index.FormatQuery("wrong", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "wrong"})
	require.NoError(t, err)
	require.Equal(t, "a__one", name)
	require.GreaterOrEqual(t, dec.Score, 0.99)
	require.Equal(t, OutcomeVectorHit, dec.Outcome)
}

func TestSemanticRouterRejectRenameWhenDisabled(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.Tool{Name: "a__one", Description: "first", ParamKeys: nil}
	emb.vecs[index.FormatDocument(t1)] = []float32{1, 0, 0, 0}
	q := index.FormatQuery("typo", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	cfg.AllowAutoRename = false
	e := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, e, context.Background(), "v1", []CatalogEntry{{Tool: t1, UpstreamID: "b1"}})

	_, _, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "typo"})
	require.Error(t, err)
}

func TestSemanticRouterAllowedToolsFilter(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.Tool{Name: "a__one", Description: "first", ParamKeys: nil}
	t2 := index.Tool{Name: "a__two", Description: "second", ParamKeys: nil}
	emb.vecs[index.FormatDocument(t1)] = []float32{1, 0, 0, 0}
	emb.vecs[index.FormatDocument(t2)] = []float32{0.9, 0.1, 0, 0} // closer to query in raw space
	q := index.FormatQuery("x", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.5
	cfg.AllowAutoRename = true
	e := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, e, context.Background(), "v1", []CatalogEntry{
		{Tool: t1, UpstreamID: "b1"},
		{Tool: t2, UpstreamID: "b1"},
	})

	name, _, err := e.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:  "x",
		AllowList: []string{"a__one"},
	})
	require.NoError(t, err)
	require.Equal(t, "a__one", name)
}

func TestSemanticRouterRulesAliasExact(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	e := NewSemanticRouter(cfg, emb, st, dim)
	e.SetRules(rules.New(map[string]string{"legacy__logs": "pre__tool"}, nil))
	row := index.Tool{Name: "pre__tool", Description: "d", ParamKeys: nil}
	reindexAndApply(t, e, context.Background(), "v1", []CatalogEntry{{Tool: row, UpstreamID: "be1"}})

	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "legacy__logs"})
	require.NoError(t, err)
	require.Equal(t, "pre__tool", name)
	require.Equal(t, "rules", dec.FallbackLayer)
	require.Equal(t, OutcomeRulesAlias, dec.Outcome)
}

func TestSemanticRouterModeOff(t *testing.T) {
	e := NewSemanticRouter(DefaultSemanticRouterRuntimeConfig(), nil, nil, 4)
	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "any"})
	require.NoError(t, err)
	require.Equal(t, "any", name)
	require.Equal(t, "none", dec.FallbackLayer)
	require.Equal(t, OutcomeNone, dec.Outcome)
}

func TestNilSemanticRouterSurface(t *testing.T) {
	var e *SemanticRouter
	require.False(t, e.Enabled())
	require.NoError(t, e.Reindex(context.Background(), "v", nil))
	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "x"})
	require.NoError(t, err)
	require.Equal(t, "x", name)
	require.Equal(t, OutcomeNone, dec.Outcome)
}

func TestDefaultSemanticRouterRuntimeEmbedTimeout(t *testing.T) {
	c := DefaultSemanticRouterRuntimeConfig()
	require.NotZero(t, c.EmbedTimeout)
	require.Equal(t, mode.Off, c.Mode)
}

func TestSemanticRouterReindexRequiresEmbed(t *testing.T) {
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	e := NewSemanticRouter(cfg, nil, store.NewInMemoryVectorStore(4), 4)
	err := e.Reindex(context.Background(), "v1", []CatalogEntry{{Tool: index.Tool{Name: "a__b"}, UpstreamID: "x"}})
	require.Error(t, err)
}

func TestReindexNoOpWhenRouterDisabled(t *testing.T) {
	e := NewSemanticRouter(DefaultSemanticRouterRuntimeConfig(), nil, store.NewInMemoryVectorStore(4), 4)
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{
		{Tool: index.Tool{Name: "a__b"}, UpstreamID: "x"},
	}))
}

func TestReindexRejectsEmptyCatalogVersion(t *testing.T) {
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	e := NewSemanticRouter(cfg, &mapEmbed{dim: 4}, store.NewInMemoryVectorStore(4), 4)
	err := e.Reindex(context.Background(), "", []CatalogEntry{
		{Tool: index.Tool{Name: "a__b"}, UpstreamID: "x"},
	})
	require.Error(t, err)
}

func TestSemanticRouterRejectsStaleClientCatalogPin(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	e := NewSemanticRouter(cfg, emb, st, dim)
	row := index.Tool{Name: "pre__tool", Description: "d", ParamKeys: nil}
	reindexAndApply(t, e, context.Background(), "v2", []CatalogEntry{{Tool: row, UpstreamID: "be1"}})

	_, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:       "pre__tool",
		CatalogVersion: "v1",
	})
	require.Error(t, err)
	require.Equal(t, OutcomeMissStaleCatalog, dec.Outcome)
}

func TestBuildIndexedToolsUpstreamLookupError(t *testing.T) {
	raw := []byte(`{"tools":[{"name":"p__t","description":"","inputSchema":{"type":"object"}}]}`)
	_, err := BuildIndexedTools(raw, func(string) (string, error) {
		return "", fmt.Errorf("no upstream")
	})
	require.Error(t, err)
}

func TestSemanticRouterVectorQueryUsesArgumentKeysFromPayload(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.Tool{Name: "a__one", Description: "first", ParamKeys: nil}
	emb.vecs[index.FormatDocument(t1)] = []float32{1, 0, 0, 0}
	q := index.FormatQuery("typo", "", []string{"k"})
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	cfg.AllowAutoRename = true
	e := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, e, context.Background(), "v1", []CatalogEntry{{Tool: t1, UpstreamID: "b1"}})

	name, _, err := e.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:      "typo",
		ArgumentsJSON: json.RawMessage(`{"k":true}`),
	})
	require.NoError(t, err)
	require.Equal(t, "a__one", name)
}
