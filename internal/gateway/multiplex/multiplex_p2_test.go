package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
)

func TestResourcesListAggregatesNamespacedURIs(t *testing.T) {
	a1 := mock.NewMockUpstream("u1", "alpha", []string{"echo"})
	a1.OmitResourcesList = false
	a1.ResourceURIs = []string{"file:///a"}
	a2 := mock.NewMockUpstream("u2", "beta", []string{"ping"})
	a2.OmitResourcesList = false
	a2.ResourceURIs = []string{"memo://b"}

	m, err := New([]backend.Upstream{a1, a2})
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
	a1 := mock.NewMockUpstream("u1", "alpha", []string{"echo"})
	a1.OmitResourcesList = false
	a1.ResourceURIs = []string{"file:///doc"}
	m, err := New([]backend.Upstream{a1})
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
	a1 := mock.NewMockUpstream("u1", "p", []string{"t"})
	a1.OmitPromptsList = false
	a1.PromptNames = []string{"greet"}
	m, err := New([]backend.Upstream{a1})
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

func TestInitializeStrictFailsOnUpstreamJSONRPCError(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "a", []string{"t"})
	b2 := mock.NewMockUpstream("b2", "b", []string{"x"})
	b2.InitJSONRPCMessage = "init failed"
	m, err := New([]backend.Upstream{b1, b2}, WithAggregationStrict(true, false))
	require.NoError(t, err)
	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestToolsListStrictFailsOnPartialBackend(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "a", []string{"t"})
	b2 := mock.NewMockUpstream("b2", "b", []string{"x"})
	b2.ToolsListJSONRPCMessage = "list down"
	m, err := New([]backend.Upstream{b1, b2}, WithAggregationStrict(false, true))
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestResourcesListStrictFailsOnPartialBackend(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "a", []string{"t"})
	b1.OmitResourcesList = false
	b1.ResourceURIs = []string{"file:///x"}
	b2 := mock.NewMockUpstream("b2", "b", []string{"x"})
	b2.OmitResourcesList = false
	b2.ResourceURIs = []string{"file:///y"}
	b2.ResourcesListJSONRPCMessage = "res list down"
	m, err := New([]backend.Upstream{b1, b2}, WithAggregationStrict(false, true))
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.ResourcesList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestPromptsListStrictFailsOnPartialBackend(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"t"})
	b1.OmitPromptsList = false
	b1.PromptNames = []string{"one"}
	b2 := mock.NewMockUpstream("b2", "q", []string{"t"})
	b2.OmitPromptsList = false
	b2.PromptNames = []string{"two"}
	b2.PromptsListJSONRPCMessage = "prompts down"
	m, err := New([]backend.Upstream{b1, b2}, WithAggregationStrict(false, true))
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.PromptsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}
