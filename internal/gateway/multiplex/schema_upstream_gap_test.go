package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type listFailsAfterFirstUpstream struct {
	upstream.Client
	lists atomic.Int32
}

func (u *listFailsAfterFirstUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == mcpwire.MethodToolsList && u.lists.Add(1) > 1 {
		return nil, errors.New("upstream down")
	}
	return u.Client.Call(ctx, req)
}

func TestARefreshKeepsTheValidatorsOfAnUpstreamThatMissedIt(t *testing.T) {
	beta := &listFailsAfterFirstUpstream{Client: mock.NewMockUpstreamWith("b2", "beta", []string{"search"}, mock.Behaviour{
		InputSchemaByTool: map[string]map[string]any{"search": {
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{"q": map[string]any{"type": "string"}},
			"required":             []any{"q"},
		}},
	})}
	m, err := New(context.Background(),
		[]upstream.Client{mock.NewMockUpstream("b1", "alpha", []string{"echo"}), beta},
		WithListTTL(0))
	require.NoError(t, err)

	_, err = m.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	_, err = m.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.EqualValues(t, 2, beta.lists.Load(), "the second list must really have reached beta")

	params, err := json.Marshal(map[string]any{"name": "beta__search", "arguments": map[string]any{"wrong": 1}})
	require.NoError(t, err)
	resp, err := m.ToolsCall(context.Background(), json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error,
		"a tool whose upstream missed one refresh must still have its arguments validated")
	require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
}
