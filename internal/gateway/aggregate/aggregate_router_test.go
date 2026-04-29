package aggregate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
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
		var v []float32
		if x, ok := m.vecs[t]; ok {
			v = append([]float32(nil), x...)
		} else {
			v = make([]float32, m.dim)
			v[0] = 1
		}
		store.L2Normalize(v)
		out[i] = v
	}
	return out, nil
}

type embedCtxDone struct {
	inner *mapEmbed
}

func (e *embedCtxDone) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.inner.Embed(ctx, texts)
}

func routerTestEngine(t *testing.T, emb embed.Embedder, scoreMin float64, autoRename bool) (*router.Engine, *store.Memory) {
	t.Helper()
	st := store.NewMemory(4)
	cfg := router.DefaultConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 8
	cfg.ScoreMin = scoreMin
	cfg.AllowAutoRename = autoRename
	cfg.EmbedTimeout = 5 * time.Second
	cfg.QueryTimeout = 5 * time.Second
	return router.NewEngine(cfg, emb, st, 4), st
}

func TestToolsListReindexAfterErrgroupDoesNotUseCanceledContext(t *testing.T) {
	b1 := mock.New("b1", "p", []string{"echo"})
	base := &mapEmbed{dim: 4, vecs: map[string][]float32{}}
	emb := &embedCtxDone{inner: base}
	eng, _ := routerTestEngine(t, emb, 0.99, true)

	a, err := New([]backend.Backend{b1}, WithListTTL(0), WithSemanticRouter(eng))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, err = a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)

	params, _ := json.Marshal(map[string]any{"name": "p__echo", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.Equal(t, "echo", b1.LastNativeTool())
}

func TestAggregateSemanticRouterExactMatch(t *testing.T) {
	b1 := mock.New("b1", "p", []string{"echo"})
	emb := &mapEmbed{dim: 4, vecs: map[string][]float32{}}
	eng, _ := routerTestEngine(t, emb, 0.99, true)

	a, err := New([]backend.Backend{b1}, WithListTTL(0), WithSemanticRouter(eng))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, err = a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)

	params, _ := json.Marshal(map[string]any{"name": "p__echo", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.Equal(t, "echo", b1.LastNativeTool())
}

func TestAggregateSemanticRouterAmbiguousReturnsCode(t *testing.T) {
	b1 := mock.New("b1", "p", []string{"echo", "list"})
	emb := &mapEmbed{dim: 4, vecs: map[string][]float32{}}
	eng, _ := routerTestEngine(t, emb, 0.5, true)

	a, err := New([]backend.Backend{b1}, WithListTTL(0), WithSemanticRouter(eng))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, err = a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)

	params, _ := json.Marshal(map[string]any{"name": "vague", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`9`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.ToolRoutingAmbiguous, resp.Error.Code)
}
