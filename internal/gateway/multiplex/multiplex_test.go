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

type initFailBackend struct {
	inner backend.Upstream
}

func (f *initFailBackend) ID() string     { return f.inner.ID() }
func (f *initFailBackend) Prefix() string { return f.inner.Prefix() }
func (f *initFailBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "initialize" {
		return nil, errors.New("simulated unreachable")
	}
	return f.inner.Call(ctx, req)
}

type listFailBackend struct {
	inner backend.Upstream
}

func (f *listFailBackend) ID() string     { return f.inner.ID() }
func (f *listFailBackend) Prefix() string { return f.inner.Prefix() }
func (f *listFailBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		return nil, errors.New("list down")
	}
	return f.inner.Call(ctx, req)
}

type errReplyBackend struct {
	inner *mock.MockUpstream
}

func (e *errReplyBackend) ID() string     { return e.inner.ID() }
func (e *errReplyBackend) Prefix() string { return e.inner.Prefix() }
func (e *errReplyBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/call" {
		return rpc.NewError(req.ID, 99, "backend says no", nil), nil
	}
	return e.inner.Call(ctx, req)
}

func TestInitializeMergeTwoBackends(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New([]backend.Upstream{b1, b2}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var merged map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &merged))
	extras := merged["serverInfo"].(map[string]any)["extras"].(map[string]any)
	backends := extras["backends"].([]any)
	require.Len(t, backends, 2)
}

func TestInitializeOmitsFailedBackend(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New([]backend.Upstream{
		b1,
		&initFailBackend{inner: b2},
	}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var merged map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &merged))
	extras := merged["serverInfo"].(map[string]any)["extras"].(map[string]any)
	backends := extras["backends"].([]any)
	require.Len(t, backends, 1)
	require.Equal(t, "b1", backends[0])
}

func TestInitializeAllBackendsFail(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New([]backend.Upstream{
		&initFailBackend{inner: b1},
		&initFailBackend{inner: b2},
	}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`7`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
	require.JSONEq(t, `7`, string(resp.ID))
}

func TestToolsListOmitsFailedBackend(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New([]backend.Upstream{
		b1,
		&listFailBackend{inner: b2},
	}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.ToolsList(context.Background(), json.RawMessage(`2`))
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

func TestToolsListOrderByConfigThenNative(t *testing.T) {
	z := mock.NewMockUpstream("z", "zebra", []string{"z2", "z1"})
	ap := mock.NewMockUpstream("a", "apple", []string{"a1"})
	a, err := New([]backend.Upstream{z, ap}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var body struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	names := make([]string, len(body.Tools))
	for i, t := range body.Tools {
		names[i] = t.Name
	}
	require.Equal(t, []string{"zebra__z1", "zebra__z2", "apple__a1"}, names)
}

func TestToolsCallStripsPrefixPreservesID(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p1", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "p2", []string{"ping"})
	a, err := New([]backend.Upstream{b1, b2}, WithListTTL(0))
	require.NoError(t, err)

	params := map[string]any{
		"name":      "p2__ping",
		"arguments": map[string]any{},
	}
	pb, _ := json.Marshal(params)

	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`99`), pb)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.JSONEq(t, `99`, string(resp.ID))

	require.Equal(t, "ping", b2.LastNativeTool())
	require.Equal(t, "", b1.LastNativeTool())
}

func TestToolsCallForwardsBackendJSONRPCErrorPreservesID(t *testing.T) {
	good := mock.NewMockUpstream("b1", "p1", []string{"echo"})
	errBack := &errReplyBackend{inner: mock.NewMockUpstream("b2", "p2", []string{"ping"})}
	a, err := New([]backend.Upstream{good, errBack}, WithListTTL(0))
	require.NoError(t, err)

	params := map[string]any{"name": "p2__ping", "arguments": map[string]any{}}
	pb, _ := json.Marshal(params)

	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`42`), pb)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, 99, resp.Error.Code)
	require.JSONEq(t, `42`, string(resp.ID))
}
