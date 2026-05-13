package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestInitializeOmitsPartialFailuresByDefault(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.InitTransportErr = errors.New("unreachable")
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	extras := mustInitExtras(t, resp.Result)
	_, has := extras["partial_failures"]
	require.False(t, has)
}

func TestInitializeReportsPartialFailuresWhenEnabled(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.InitTransportErr = errors.New("unreachable")
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	failures := mustPartialFailures(t, mustInitExtras(t, resp.Result))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureTransport, failures[0]["reason"])
}

func TestInitializeReportsJSONRPCPartialFailure(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.InitJSONRPCMessage = "init failed"
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	failures := mustPartialFailures(t, mustInitExtras(t, resp.Result))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureJSONRPC, failures[0]["reason"])
}

func TestInitializeStrictOmitsPartialFailuresMetadata(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.InitJSONRPCMessage = "init failed"
	m, err := New([]backend.Upstream{b1, b2}, WithAggregationStrict(true, false), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestToolsListReportsPartialFailuresWhenEnabled(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.ToolsListJSONRPCMessage = "list down"
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	failures := mustPartialFailures(t, body["extras"].(map[string]any))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureJSONRPC, failures[0]["reason"])
}

func TestToolsListStrictOmitsPartialFailuresMetadata(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.ToolsListJSONRPCMessage = "list down"
	m, err := New([]backend.Upstream{b1, b2}, WithAggregationStrict(false, true), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.StrictAggregationFailed, resp.Error.Code)
}

func TestInitializeReportsTimeoutPartialFailure(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	b2.InitTransportErr = context.DeadlineExceeded
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	failures := mustPartialFailures(t, mustInitExtras(t, resp.Result))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureTimeout, failures[0]["reason"])
}

func TestToolsListReportsTimeoutPartialFailure(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := &timeoutOnMethodBackend{
		inner:  mock.NewMockUpstream("b2", "beta", []string{"ping"}),
		method: "tools/list",
		err:    context.DeadlineExceeded,
	}
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	failures := mustPartialFailures(t, body["extras"].(map[string]any))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureTimeout, failures[0]["reason"])
}

func TestResourcesListReportsPartialFailuresWhenEnabled(t *testing.T) {
	b1 := mockUpstreamWithResources("b1", "alpha", []string{"file:///a"})
	b2 := mockUpstreamWithResources("b2", "beta", []string{"file:///b"})
	b2.ResourcesListJSONRPCMessage = "resources down"
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.ResourcesList(context.Background(), json.RawMessage(`3`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	failures := mustPartialFailures(t, body["extras"].(map[string]any))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureJSONRPC, failures[0]["reason"])
}

func TestPromptsListReportsPartialFailuresWhenEnabled(t *testing.T) {
	b1 := mockUpstreamWithPrompts("b1", "alpha", []string{"summarize"})
	b2 := mockUpstreamWithPrompts("b2", "beta", []string{"review"})
	b2.PromptsListJSONRPCMessage = "prompts down"
	m, err := New([]backend.Upstream{b1, b2}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := m.PromptsList(context.Background(), json.RawMessage(`4`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	failures := mustPartialFailures(t, body["extras"].(map[string]any))
	require.Len(t, failures, 1)
	require.Equal(t, "b2", failures[0]["backend_id"])
	require.Equal(t, PartialFailureJSONRPC, failures[0]["reason"])
}

type timeoutOnMethodBackend struct {
	inner  backend.Upstream
	method string
	err    error
}

func (t *timeoutOnMethodBackend) ID() string     { return t.inner.ID() }
func (t *timeoutOnMethodBackend) Prefix() string { return t.inner.Prefix() }

func (t *timeoutOnMethodBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == t.method {
		return nil, t.err
	}
	return t.inner.Call(ctx, req)
}

func mockUpstreamWithResources(id, prefix string, uris []string) *mock.MockUpstream {
	m := mock.NewMockUpstream(id, prefix, nil)
	m.OmitResourcesList = false
	m.ResourceURIs = append([]string(nil), uris...)
	return m
}

func mockUpstreamWithPrompts(id, prefix string, names []string) *mock.MockUpstream {
	m := mock.NewMockUpstream(id, prefix, nil)
	m.OmitPromptsList = false
	m.PromptNames = append([]string(nil), names...)
	return m
}

func mustInitExtras(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var merged map[string]any
	require.NoError(t, json.Unmarshal(raw, &merged))
	extras := merged["serverInfo"].(map[string]any)["extras"].(map[string]any)
	return extras
}

func mustPartialFailures(t *testing.T, extras map[string]any) []map[string]any {
	t.Helper()
	raw, ok := extras["partial_failures"].([]any)
	require.True(t, ok)
	out := make([]map[string]any, len(raw))
	for i, item := range raw {
		out[i] = item.(map[string]any)
	}
	return out
}
