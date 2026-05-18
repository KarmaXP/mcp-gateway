package multiplex

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type nilUpstreamBackend struct{ inner backend.Upstream }

func (n *nilUpstreamBackend) ID() string     { return n.inner.ID() }
func (n *nilUpstreamBackend) Prefix() string { return n.inner.Prefix() }
func (n *nilUpstreamBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	switch req.Method {
	case "tools/list", "initialize", "tools/call":
		return nil, nil
	default:
		return n.inner.Call(ctx, req)
	}
}

func TestToolsListNilUpstreamResponseNoPanic(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	up := &nilUpstreamBackend{inner: inner}
	mpx, err := New([]backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage([]byte("0")))
	require.NoError(t, err)
	resp, err := mpx.ToolsList(context.Background(), json.RawMessage([]byte("1")))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
}

func TestInitializeNilUpstreamResponseJSONRPCError(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	up := &nilUpstreamBackend{inner: inner}
	mpx, err := New([]backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	resp, err := mpx.Initialize(context.Background(), json.RawMessage([]byte("1")))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

func TestToolsCallNilUpstreamResponseJSONRPCError(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	up := &nilUpstreamBackend{inner: inner}
	mpx, err := New([]backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage([]byte("0")))
	require.NoError(t, err)
	_, err = mpx.ToolsList(context.Background(), json.RawMessage([]byte("1")))
	require.NoError(t, err)
	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := mpx.ToolsCall(context.Background(), json.RawMessage([]byte("2")), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

type countingInitializeBackend struct {
	inner backend.Upstream
	calls atomic.Int32
}

func (c *countingInitializeBackend) ID() string     { return c.inner.ID() }
func (c *countingInitializeBackend) Prefix() string { return c.inner.Prefix() }
func (c *countingInitializeBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "initialize" {
		c.calls.Add(1)
	}
	return c.inner.Call(ctx, req)
}

func TestInitializeIdempotentSkipsRepeatUpstreamCall(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	initBack := &countingInitializeBackend{inner: inner}
	listBack := &countingListBackend{inner: initBack}
	mpx, err := New([]backend.Upstream{listBack}, WithListTTL(time.Minute))
	require.NoError(t, err)
	_, err = mpx.Initialize(context.Background(), json.RawMessage([]byte("1")))
	require.NoError(t, err)
	require.Equal(t, int32(1), initBack.calls.Load())
	_, err = mpx.ToolsList(context.Background(), json.RawMessage([]byte("2")))
	require.NoError(t, err)
	require.Equal(t, int32(1), listBack.calls.Load())
	_, err = mpx.Initialize(context.Background(), json.RawMessage([]byte("3")))
	require.NoError(t, err)
	require.Equal(t, int32(1), initBack.calls.Load())
	_, err = mpx.ToolsList(context.Background(), json.RawMessage([]byte("4")))
	require.NoError(t, err)
	require.Equal(t, int32(1), listBack.calls.Load())
}

type initParamsRecorder struct {
	inner  backend.Upstream
	mu     sync.Mutex
	params json.RawMessage
}

func (r *initParamsRecorder) ID() string     { return r.inner.ID() }
func (r *initParamsRecorder) Prefix() string { return r.inner.Prefix() }
func (r *initParamsRecorder) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "initialize" {
		r.mu.Lock()
		r.params = append(json.RawMessage(nil), req.Params...)
		r.mu.Unlock()
	}
	return r.inner.Call(ctx, req)
}

func TestInitializeForwardsHostParamsToUpstream(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	rec := &initParamsRecorder{inner: inner}
	mpx, err := New([]backend.Upstream{rec}, WithListTTL(0))
	require.NoError(t, err)
	hostParams, _ := json.Marshal(map[string]any{
		"protocolVersion": "2099-01-01",
		"capabilities":    map[string]any{"experimental": map[string]any{}},
		"clientInfo":      map[string]any{"name": "custom-host", "version": "9.9.9"},
	})
	ctx := WithHostInitializeParams(context.Background(), hostParams)
	_, err = mpx.Initialize(ctx, json.RawMessage([]byte("1")))
	require.NoError(t, err)
	rec.mu.Lock()
	got := append(json.RawMessage(nil), rec.params...)
	rec.mu.Unlock()
	var gotMap map[string]any
	require.NoError(t, json.Unmarshal(got, &gotMap))
	require.Equal(t, "2099-01-01", gotMap["protocolVersion"])
}

func TestPolicyHardenSchemasInsideAllOf(t *testing.T) {
	inputSchema := map[string]any{
		"allOf": []any{map[string]any{
			"type":       "object",
			"properties": map[string]any{"msg": map[string]any{"type": "string"}},
			"required":   []any{"msg"},
		}},
	}
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b1.InputSchemaByTool = map[string]map[string]any{"echo": inputSchema}
	pol := policy.NewEngine(config.PolicySettings{Version: "t", HardenSchemas: true, ElevatedTools: []string{"alpha__echo"}})
	a, err := New([]backend.Upstream{b1}, WithListTTL(0), WithPolicyEngine(pol))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage([]byte("1")))
	_, _ = a.ToolsList(context.Background(), json.RawMessage([]byte("2")))
	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{"msg": "hi", "extra": "nope"}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage([]byte("3")), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.InvalidParams, resp.Error.Code)
}
