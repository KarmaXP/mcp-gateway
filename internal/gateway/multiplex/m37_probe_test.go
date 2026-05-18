package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
)

func TestM37ProbeRestrictedDisallowedWithRouter(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "list"})
	base := &mapEmbed{dim: 4, vecs: make(map[string][]float32)}
	tRowList := index.FormatDocument(index.ToolRow{Name: "alpha__list", Description: "list tool"})
	tRowEcho := index.FormatDocument(index.ToolRow{Name: "alpha__echo", Description: "echo tool"})
	base.vecs[tRowList] = []float32{1, 0, 0, 0}
	base.vecs[tRowEcho] = []float32{0, 1, 0, 0}

	sr, _ := routerTestSemanticRouter(t, base, 0.5, false)
	a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithSemanticRouter(sr))
	require.NoError(t, err)
	_, err = a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	_, err = a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__echo"})
	params, _ := json.Marshal(map[string]any{"name": "alpha__list", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(ctx, json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	t.Logf("got code=%d msg=%s", resp.Error.Code, resp.Error.Message)
	require.Equal(t, errcodes.PermissionDenied, resp.Error.Code)
	require.Equal(t, uint64(0), b1.ToolsCallInvocationCount())
}

var _ backend.Upstream = (*mock.MockUpstream)(nil)
