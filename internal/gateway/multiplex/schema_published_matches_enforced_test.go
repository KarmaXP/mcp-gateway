package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func publishedInputSchema(t *testing.T, result json.RawMessage, tool string) map[string]any {
	t.Helper()
	var payload struct {
		Tools []map[string]any `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result, &payload))
	for _, entry := range payload.Tools {
		if name, _ := entry["name"].(string); name == tool {
			schema, _ := entry["inputSchema"].(map[string]any)
			return schema
		}
	}
	t.Fatalf("tool %q is missing from tools/list", tool)
	return nil
}

func TestPublishedToolSchemaIsTheOneTheGatewayEnforces(t *testing.T) {
	enumerated := map[string]any{
		"type":       "object",
		"properties": map[string]any{"msg": map[string]any{"type": "string"}},
	}

	tests := []struct {
		name              string
		inputSchema       map[string]any
		allowOpenSchemas  bool
		wantAdditionalSet bool
		wantCallRejected  bool
	}{
		{
			name:              "an enumerated shape is advertised closed and rejects an undeclared argument",
			inputSchema:       enumerated,
			wantAdditionalSet: true,
			wantCallRejected:  true,
		},
		{
			name:              "a schema enumerating nothing is advertised open and accepts anything",
			inputSchema:       map[string]any{"type": "object"},
			wantAdditionalSet: false,
			wantCallRejected:  false,
		},
		{
			name:              "with hardening disabled the schema is advertised open and the argument is accepted",
			inputSchema:       enumerated,
			allowOpenSchemas:  true,
			wantAdditionalSet: false,
			wantCallRejected:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b1 := mock.NewMockUpstreamWith("b1", "alpha", []string{"echo"}, mock.Behaviour{
				InputSchemaByTool: map[string]map[string]any{"echo": tc.inputSchema},
			})
			pol := policy.NewEngine(policy.EngineInput{Version: "t", AllowOpenSchemas: tc.allowOpenSchemas})
			a, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), withPolicyEngine(pol))
			require.NoError(t, err)

			_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
			listed, err := a.ToolsList(context.Background(), json.RawMessage(`2`))
			require.NoError(t, err)
			require.Nil(t, listed.Error)

			schema := publishedInputSchema(t, listed.Result, "alpha__echo")
			additional, present := schema["additionalProperties"]
			if tc.wantAdditionalSet {
				require.True(t, present,
					"the host is served a schema that permits undeclared arguments the gateway then rejects")
				require.Equal(t, false, additional)
			} else {
				require.False(t, present,
					"the host is served a closed schema for arguments the gateway would have accepted")
			}

			params, _ := json.Marshal(map[string]any{
				"name":      "alpha__echo",
				"arguments": map[string]any{"msg": "hi", "extra": "nope"},
			})
			called, err := a.ToolsCall(context.Background(), json.RawMessage(`3`), params)
			require.NoError(t, err)
			if tc.wantCallRejected {
				require.NotNil(t, called.Error)
				require.Equal(t, errcodes.InvalidParams, called.Error.Code)
				return
			}
			require.Nil(t, called.Error)
		})
	}
}

func TestCachedToolsListKeepsThePublishedSchemaClosed(t *testing.T) {
	b1 := mock.NewMockUpstreamWith("b1", "alpha", []string{"echo"}, mock.Behaviour{
		InputSchemaByTool: map[string]map[string]any{"echo": {
			"type":       "object",
			"properties": map[string]any{"msg": map[string]any{"type": "string"}},
		}},
	})
	a, err := New(context.Background(), []upstream.Client{b1})
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))

	first, err := a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	second, err := a.ToolsList(context.Background(), json.RawMessage(`3`))
	require.NoError(t, err)

	require.Equal(t,
		publishedInputSchema(t, first.Result, "alpha__echo"),
		publishedInputSchema(t, second.Result, "alpha__echo"),
		"a cached tools/list must advertise the same schema as the one that filled the cache")
	require.Equal(t, false, publishedInputSchema(t, second.Result, "alpha__echo")["additionalProperties"])
}
