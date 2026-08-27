package multiplex

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func enumeratedSchemaUpstream() *mock.MockUpstream {
	return mock.NewMockUpstreamWith("b1", "alpha", []string{"echo"}, mock.Behaviour{
		InputSchemaByTool: map[string]map[string]any{"echo": {
			"type":       "object",
			"properties": map[string]any{"msg": map[string]any{"type": "string"}},
		}},
	})
}

func TestHardeningNeverReachesTheUpstreamsOwnSchema(t *testing.T) {
	b1 := enumeratedSchemaUpstream()

	hardening, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = hardening.Initialize(context.Background(), json.RawMessage(`1`))
	closed, err := hardening.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Equal(t, false, publishedInputSchema(t, closed.Result, "alpha__echo")["additionalProperties"])

	open := policy.NewEngine(policy.EngineInput{Version: "t", AllowOpenSchemas: true})
	relaxed, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), withPolicyEngine(open))
	require.NoError(t, err)
	_, _ = relaxed.Initialize(context.Background(), json.RawMessage(`3`))
	untouched, err := relaxed.ToolsList(context.Background(), json.RawMessage(`4`))
	require.NoError(t, err)

	_, present := publishedInputSchema(t, untouched.Result, "alpha__echo")["additionalProperties"]
	require.False(t, present,
		"hardening escaped the response and changed the upstream's own schema for every later reader")
}

func TestConcurrentListsAllSeeTheClosedSchema(t *testing.T) {
	a, err := New(context.Background(), []upstream.Client{enumeratedSchemaUpstream()}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))

	const workers = 24
	results := make([]any, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := json.RawMessage(`"L"`)
			listed, listErr := a.ToolsList(context.Background(), id)
			if listErr == nil && listed.Error == nil {
				results[n] = publishedInputSchema(t, listed.Result, "alpha__echo")["additionalProperties"]
			}
			params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{"msg": "x"}})
			_, _ = a.ToolsCall(context.Background(), id, params)
			if n%3 == 0 {
				a.HandleToolsListChanged()
			}
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		require.Equal(t, false, got,
			"worker %d was served a schema the gateway would not have enforced", i)
	}
}
