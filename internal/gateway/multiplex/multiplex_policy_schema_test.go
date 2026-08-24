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
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

func TestToolsListFilteredByJWTAllowList(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "list"})
	a, err := New([]backend.Upstream{b1}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__echo"})
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
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "list"})
	a, err := New([]backend.Upstream{b1}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__echo"})
	params, _ := json.Marshal(map[string]any{"name": "alpha__list", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(ctx, json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.PermissionDenied, resp.Error.Code)
}

func TestToolsCallValidatesArgumentsAgainstSchema(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
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
	a, err := New([]backend.Upstream{b1}, WithListTTL(0))
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

func TestToolsCallElevatedToolRequiresSchema(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	pol := policy.NewEngine(policy.EngineInput{
		Version:       "t",
		ElevatedTools: []string{"alpha__echo"},
	})
	a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithPolicyEngine(pol))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{"msg": "hi"}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`9`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
}

func TestToolsCallHardensElevatedObjectSchemas(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
		"required": []any{"msg"},
	}
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b1.InputSchemaByTool = map[string]map[string]any{
		"echo": inputSchema,
	}
	pol := policy.NewEngine(policy.EngineInput{
		Version:       "t",
		ElevatedTools: []string{"alpha__echo"},
	})
	a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithPolicyEngine(pol))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	_, mutated := inputSchema["additionalProperties"]
	require.False(t, mutated, "source upstream schema should not be mutated")

	params, _ := json.Marshal(map[string]any{
		"name": "alpha__echo",
		"arguments": map[string]any{
			"msg":   "hi",
			"extra": "nope",
		},
	})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
}

func TestToolsCallDoesNotHardenSchemaWhenPolicyDisabled(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"msg": map[string]any{"type": "string"},
		},
		"required": []any{"msg"},
	}
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b1.InputSchemaByTool = map[string]map[string]any{
		"echo": inputSchema,
	}
	pol := policy.NewEngine(policy.EngineInput{
		Version:          "t",
		AllowOpenSchemas: true,
		ElevatedTools:    []string{"alpha__echo"},
	})
	a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithPolicyEngine(pol))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	params, _ := json.Marshal(map[string]any{
		"name": "alpha__echo",
		"arguments": map[string]any{
			"msg":   "hi",
			"extra": "allowed-when-not-hardened",
		},
	})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
}

func TestToolsCallHardensSchemasByDefaultForEveryTool(t *testing.T) {
	tests := []struct {
		name        string
		inputSchema map[string]any
		arguments   map[string]any
		wantError   bool
	}{
		{
			name: "an enumerated shape is closed even without an elevated_tools entry",
			inputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"msg": map[string]any{"type": "string"}},
			},
			arguments: map[string]any{"msg": "hi", "extra": "nope"},
			wantError: true,
		},
		{
			name:        "a schema that enumerates nothing stays open",
			inputSchema: map[string]any{"type": "object"},
			arguments:   map[string]any{"anything": "goes"},
			wantError:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
			b1.InputSchemaByTool = map[string]map[string]any{"echo": tc.inputSchema}
			a, err := New([]backend.Upstream{b1}, WithListTTL(0))
			require.NoError(t, err)
			_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
			_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

			params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": tc.arguments})
			resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
			require.NoError(t, err)
			if tc.wantError {
				require.NotNil(t, resp.Error)
				require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
				return
			}
			require.Nil(t, resp.Error)
		})
	}
}

func TestElevatedToolNeedsASchemaThatDeclaresSomething(t *testing.T) {
	tests := []struct {
		name        string
		inputSchema map[string]any
		wantErr     string
	}{
		{
			name:        "a schema that enumerates nothing does not satisfy the rule",
			inputSchema: map[string]any{"type": "object"},
			wantErr:     "requires an input schema that declares its properties",
		},
		{
			name: "an enumerated schema does",
			inputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"msg": map[string]any{"type": "string"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
			b1.InputSchemaByTool = map[string]map[string]any{"echo": tc.inputSchema}
			pol := policy.NewEngine(policy.EngineInput{Version: "t", ElevatedTools: []string{"alpha__echo"}})
			a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithPolicyEngine(pol))
			require.NoError(t, err)
			_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
			_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

			params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{"msg": "hi"}})
			resp, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
			require.NoError(t, err)
			if tc.wantErr == "" {
				require.Nil(t, resp.Error)
				return
			}
			require.NotNil(t, resp.Error, "an elevated tool must not run unvalidated arguments")
			require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
			require.Contains(t, resp.Error.Message, tc.wantErr)
		})
	}
}
