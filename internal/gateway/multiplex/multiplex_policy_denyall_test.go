package multiplex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func TestToolsListEmptyIntersectionReturnsNoTools(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "other"})
	mpx, err := New([]backend.Upstream{b1}, WithListTTL(0))
	require.NoError(t, err)

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{})
	resp, err := mpx.ToolsList(ctx, json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	assertToolsListJSONIsEmptyArray(t, resp.Result)
}

func TestToolsListDenyAllReturnsEmptyArrayNotNull(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "other"})
	mpx, err := New([]backend.Upstream{b1}, WithListTTL(0))
	require.NoError(t, err)

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{})
	resp, err := mpx.ToolsList(ctx, json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	assertToolsListJSONIsEmptyArray(t, resp.Result)
}

func assertToolsListJSONIsEmptyArray(t *testing.T, result json.RawMessage) {
	t.Helper()
	raw := string(result)
	require.Contains(t, raw, `"tools":[]`, "deny-all must marshal tools as [] not null; got %s", raw)
	var payload struct {
		Tools *[]map[string]any `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result, &payload))
	require.NotNil(t, payload.Tools)
	require.Empty(t, *payload.Tools)
}

type muxEmbedSpy struct {
	inner   *mapEmbed
	invokes atomic.Int32
}

func (s *muxEmbedSpy) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	s.invokes.Add(1)
	return s.inner.Embed(ctx, texts)
}

func TestToolsCallDenyAllSkipsSemanticRouter(t *testing.T) {
	base := &mapEmbed{dim: 4, vecs: make(map[string][]float32)}
	spy := &muxEmbedSpy{inner: base}
	tRow := index.ToolRow{Name: "alpha__echo", Description: "mock echo"}
	base.vecs[index.FormatDocument(tRow)] = []float32{1, 0, 0, 0}

	st := store.NewInMemoryVectorStore(4)
	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = router.ModeAssistList
	cfg.ScoreMin = 0.5
	sr := router.NewSemanticRouter(cfg, spy, st, 4)

	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := New([]backend.Upstream{b1}, WithListTTL(0), WithSemanticRouter(sr))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage(`0`))
	require.NoError(t, err)
	_, err = mpx.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	beforeCall := spy.invokes.Load()

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{})
	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := mpx.ToolsCall(ctx, json.RawMessage(`2`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.PermissionDenied, resp.Error.Code)
	require.Equal(t, beforeCall, spy.invokes.Load())
	require.Equal(t, uint64(0), b1.ToolsCallInvocationCount())
}

func TestToolsCallEmptyIntersectionPermissionDenied(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := New([]backend.Upstream{b1}, WithListTTL(0))
	require.NoError(t, err)

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{})
	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := mpx.ToolsCall(ctx, json.RawMessage(`2`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.PermissionDenied, resp.Error.Code)
}

func TestToolsListDenyAllSkipsUpstreamFanout(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "other"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	mpx, err := New([]backend.Upstream{b1, b2}, WithListTTL(0))
	require.NoError(t, err)

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{})
	resp, err := mpx.ToolsList(ctx, json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	assertToolsListJSONIsEmptyArray(t, resp.Result)
	require.Equal(t, uint64(0), b1.ToolsListInvocationCount())
	require.Equal(t, uint64(0), b2.ToolsListInvocationCount())
}

func TestToolsListCacheNotUsedForDenyAllPrincipal(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	mpx, err := New([]backend.Upstream{b1}, WithListTTL(time.Minute))
	require.NoError(t, err)

	openCtx := context.Background()
	_, err = mpx.ToolsList(openCtx, json.RawMessage(`1`))
	require.NoError(t, err)

	denyCtx := hostctx.WithAllowedToolNames(context.Background(), []string{})
	resp, err := mpx.ToolsList(denyCtx, json.RawMessage(`2`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	assertToolsListJSONIsEmptyArray(t, resp.Result)
}
