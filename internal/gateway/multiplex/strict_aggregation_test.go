package multiplex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestInitializeStrictFailsOnUpstreamJSONRPCError(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "a", []string{"t"})
	b2 := mock.NewMockUpstream("b2", "b", []string{"x"})
	b2.InitJSONRPCMessage = "init failed"
	m, err := New(context.Background(), []upstream.Client{b1, b2}, WithAggregationStrict(true, false))
	require.NoError(t, err)
	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestToolsListStrictFailsOnPartialUpstream(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "a", []string{"t"})
	b2 := mock.NewMockUpstream("b2", "b", []string{"x"})
	b2.ToolsListJSONRPCMessage = "list down"
	m, err := New(context.Background(), []upstream.Client{b1, b2}, WithAggregationStrict(false, true))
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestResourcesListStrictFailsOnPartialUpstream(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "a", []string{"t"})
	b1.OmitResourcesList = false
	b1.ResourceURIs = []string{"file:///x"}
	b2 := mock.NewMockUpstream("b2", "b", []string{"x"})
	b2.OmitResourcesList = false
	b2.ResourceURIs = []string{"file:///y"}
	b2.ResourcesListJSONRPCMessage = "res list down"
	m, err := New(context.Background(), []upstream.Client{b1, b2}, WithAggregationStrict(false, true))
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.ResourcesList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestPromptsListStrictFailsOnPartialUpstream(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"t"})
	b1.OmitPromptsList = false
	b1.PromptNames = []string{"one"}
	b2 := mock.NewMockUpstream("b2", "q", []string{"t"})
	b2.OmitPromptsList = false
	b2.PromptNames = []string{"two"}
	b2.PromptsListJSONRPCMessage = "prompts down"
	m, err := New(context.Background(), []upstream.Client{b1, b2}, WithAggregationStrict(false, true))
	require.NoError(t, err)
	_, _ = m.Initialize(context.Background(), json.RawMessage(`0`))
	resp, err := m.PromptsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}
