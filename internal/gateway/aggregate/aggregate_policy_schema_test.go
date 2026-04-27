package aggregate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/ingress"
)

func TestToolsListFilteredByJWTAllowList(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo", "list"})
	a, err := New([]backend.Backend{b1}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))

	ctx := ingress.WithAllowedTools(context.Background(), []string{"alpha__echo"})
	resp, err := a.ToolsList(ctx, json.RawMessage(`2`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var body struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	require.Len(t, body.Tools, 1)
	require.Equal(t, "alpha__echo", body.Tools[0].Name)
}

func TestToolsCallRejectedWhenNotInAllowList(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo", "list"})
	a, err := New([]backend.Backend{b1}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	ctx := ingress.WithAllowedTools(context.Background(), []string{"alpha__echo"})
	params, _ := json.Marshal(map[string]any{"name": "alpha__list", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(ctx, json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.RequestRejected, resp.Error.Code)
}

func TestToolsCallValidatesArgumentsAgainstSchema(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	b1.InputSchemaByTool = map[string]map[string]any{
		"echo": {
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"msg": map[string]any{"type": "string"},
			},
			"required": []any{"msg"},
		},
	}
	a, err := New([]backend.Backend{b1}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	badParams, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`4`), badParams)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.InvalidParams, resp.Error.Code)

	goodParams, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{"msg": "hi"}})
	resp, err = a.ToolsCall(context.Background(), json.RawMessage(`5`), goodParams)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.Contains(t, string(resp.Result), "ok")
}
