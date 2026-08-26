package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestResourcesListAggregatesNamespacedURIs(t *testing.T) {
	a1 := mock.NewMockUpstreamWith("u1", "alpha", []string{"echo"}, mock.Behaviour{
		SupportsResources: true,
		ResourceURIs:      []string{"file:///a"},
	})
	a2 := mock.NewMockUpstreamWith("u2", "beta", []string{"ping"}, mock.Behaviour{
		SupportsResources: true,
		ResourceURIs:      []string{"memo://b"},
	})

	m, err := New(context.Background(), []upstream.Client{a1, a2})
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.ResourcesList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var body struct {
		Resources []map[string]any `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	require.Len(t, body.Resources, 2)
	uris := make(map[string]struct{})
	for _, r := range body.Resources {
		uris[r["uri"].(string)] = struct{}{}
	}
	_, hasA := uris["alpha__file:///a"]
	_, hasB := uris["beta__memo://b"]
	require.True(t, hasA && hasB)
}

func TestResourcesReadStripsPrefixPreservesID(t *testing.T) {
	a1 := mock.NewMockUpstreamWith("u1", "alpha", []string{"echo"}, mock.Behaviour{
		SupportsResources: true,
		ResourceURIs:      []string{"file:///doc"},
	})
	m, err := New(context.Background(), []upstream.Client{a1})
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	params, err := json.Marshal(map[string]any{"uri": "alpha__file:///doc"})
	require.NoError(t, err)
	hostID := json.RawMessage(`99`)
	resp, err := m.ResourcesRead(context.Background(), hostID, params)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.JSONEq(t, string(hostID), string(resp.ID))
}

func TestPromptsListAndGet(t *testing.T) {
	a1 := mock.NewMockUpstreamWith("u1", "p", []string{"t"}, mock.Behaviour{
		SupportsPrompts: true,
		PromptNames:     []string{"greet"},
	})
	m, err := New(context.Background(), []upstream.Client{a1})
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.PromptsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var pl struct {
		Prompts []map[string]any `json:"prompts"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &pl))
	require.Len(t, pl.Prompts, 1)
	require.Equal(t, "p__greet", pl.Prompts[0]["name"])

	params, err := json.Marshal(map[string]any{"name": "p__greet"})
	require.NoError(t, err)
	resp2, err := m.PromptsGet(context.Background(), json.RawMessage(`2`), params)
	require.NoError(t, err)
	require.Nil(t, resp2.Error)
}
