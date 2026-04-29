package router

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestEngineExactShortcutNoVector(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.9
	e := NewEngine(cfg, emb, st, dim)

	row := index.ToolRow{Name: "pre__tool", Description: "d", ParamKeys: nil}
	doc := index.FormatDocument(row)
	emb.vecs[doc] = []float32{1, 0, 0, 0}

	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{
		{ToolRow: row, BackendID: "be1"},
	}))

	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "pre__tool"})
	require.NoError(t, err)
	require.Equal(t, "pre__tool", name)
	require.Equal(t, "exact", dec.FallbackLayer)
	require.Equal(t, OutcomeExact, dec.Outcome)
}

func TestEngineVectorResolvesWhenNameWrong(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: []string{"x"}}
	t2 := index.ToolRow{Name: "a__two", Description: "second", ParamKeys: nil}
	d1 := index.FormatDocument(t1)
	d2 := index.FormatDocument(t2)
	emb.vecs[d1] = []float32{1, 0, 0, 0}
	emb.vecs[d2] = []float32{0, 1, 0, 0}

	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	cfg.AllowAutoRename = true
	e := NewEngine(cfg, emb, st, dim)

	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{
		{ToolRow: t1, BackendID: "b1"},
		{ToolRow: t2, BackendID: "b1"},
	}))

	q := index.FormatQuery("wrong", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "wrong"})
	require.NoError(t, err)
	require.Equal(t, "a__one", name)
	require.GreaterOrEqual(t, dec.Confidence, 0.99)
	require.Equal(t, OutcomeVectorHit, dec.Outcome)
}

func TestEngineRejectRenameWhenDisabled(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: nil}
	emb.vecs[index.FormatDocument(t1)] = []float32{1, 0, 0, 0}
	q := index.FormatQuery("typo", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	cfg.AllowAutoRename = false
	e := NewEngine(cfg, emb, st, dim)
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{{ToolRow: t1, BackendID: "b1"}}))

	_, _, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "typo"})
	require.Error(t, err)
}

func TestEngineAllowedToolsFilter(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: nil}
	t2 := index.ToolRow{Name: "a__two", Description: "second", ParamKeys: nil}
	emb.vecs[index.FormatDocument(t1)] = []float32{1, 0, 0, 0}
	emb.vecs[index.FormatDocument(t2)] = []float32{0.9, 0.1, 0, 0} // closer to query in raw space
	q := index.FormatQuery("x", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.5
	cfg.AllowAutoRename = true
	e := NewEngine(cfg, emb, st, dim)
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{
		{ToolRow: t1, BackendID: "b1"},
		{ToolRow: t2, BackendID: "b1"},
	}))

	name, _, err := e.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:     "x",
		AllowedTools: []string{"a__one"},
	})
	require.NoError(t, err)
	require.Equal(t, "a__one", name)
}

func TestEngineRulesAliasExact(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	e := NewEngine(cfg, emb, st, dim)
	e.SetRules(rules.New(map[string]string{"legacy__logs": "pre__tool"}, nil))
	row := index.ToolRow{Name: "pre__tool", Description: "d", ParamKeys: nil}
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{{ToolRow: row, BackendID: "be1"}}))

	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "legacy__logs"})
	require.NoError(t, err)
	require.Equal(t, "pre__tool", name)
	require.Equal(t, "rules", dec.FallbackLayer)
	require.Equal(t, OutcomeRulesAlias, dec.Outcome)
}

func TestEngineModeOff(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil, nil, 4)
	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "any"})
	require.NoError(t, err)
	require.Equal(t, "any", name)
	require.Equal(t, "none", dec.FallbackLayer)
	require.Equal(t, OutcomeNone, dec.Outcome)
}

func TestNilEngineSurface(t *testing.T) {
	var e *Engine
	require.False(t, e.Enabled())
	require.NoError(t, e.Reindex(context.Background(), "v", nil))
	name, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "x"})
	require.NoError(t, err)
	require.Equal(t, "x", name)
	require.Equal(t, OutcomeNone, dec.Outcome)
}

func TestDefaultConfigEmbedTimeout(t *testing.T) {
	c := DefaultConfig()
	require.NotZero(t, c.EmbedTimeout)
	require.Equal(t, ModeOff, c.Mode)
}

func TestEngineReindexRequiresEmbed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	e := NewEngine(cfg, nil, store.NewMemory(4), 4)
	err := e.Reindex(context.Background(), "v1", []CatalogEntry{{ToolRow: index.ToolRow{Name: "a__b"}, BackendID: "x"}})
	require.Error(t, err)
}

func TestReindexNoOpWhenRouterDisabled(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil, store.NewMemory(4), 4)
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{
		{ToolRow: index.ToolRow{Name: "a__b"}, BackendID: "x"},
	}))
}

func TestReindexRejectsEmptyCatalogVersion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	e := NewEngine(cfg, &mapEmbed{dim: 4}, store.NewMemory(4), 4)
	err := e.Reindex(context.Background(), "", []CatalogEntry{
		{ToolRow: index.ToolRow{Name: "a__b"}, BackendID: "x"},
	})
	require.Error(t, err)
}

func TestEngineRejectsStaleClientCatalogPin(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: map[string][]float32{}, dim: dim}
	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	e := NewEngine(cfg, emb, st, dim)
	row := index.ToolRow{Name: "pre__tool", Description: "d", ParamKeys: nil}
	require.NoError(t, e.Reindex(context.Background(), "v2", []CatalogEntry{{ToolRow: row, BackendID: "be1"}}))

	_, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:       "pre__tool",
		CatalogVersion: "v1",
	})
	require.Error(t, err)
	require.Equal(t, OutcomeMissStaleCatalog, dec.Outcome)
}

func TestBuildCatalogEntriesBackendLookupError(t *testing.T) {
	raw := []byte(`{"tools":[{"name":"p__t","description":"","inputSchema":{"type":"object"}}]}`)
	_, err := BuildCatalogEntries(raw, func(string) (string, error) {
		return "", fmt.Errorf("no backend")
	})
	require.Error(t, err)
}

func TestEngineVectorQueryUsesArgumentKeysFromPayload(t *testing.T) {
	dim := 4
	st := store.NewMemory(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	t1 := index.ToolRow{Name: "a__one", Description: "first", ParamKeys: nil}
	emb.vecs[index.FormatDocument(t1)] = []float32{1, 0, 0, 0}
	q := index.FormatQuery("typo", "", []string{"k"})
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.99
	cfg.AllowAutoRename = true
	e := NewEngine(cfg, emb, st, dim)
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{{ToolRow: t1, BackendID: "b1"}}))

	name, _, err := e.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:      "typo",
		ArgumentsJSON: json.RawMessage(`{"k":true}`),
	})
	require.NoError(t, err)
	require.Equal(t, "a__one", name)
}
