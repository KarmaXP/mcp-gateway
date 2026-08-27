package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type initFailUpstream struct {
	inner upstream.Client
}

func (f *initFailUpstream) ID() string     { return f.inner.ID() }
func (f *initFailUpstream) Prefix() string { return f.inner.Prefix() }
func (f *initFailUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "initialize" {
		return nil, errors.New("simulated unreachable")
	}
	return f.inner.Call(ctx, req)
}

type listFailUpstream struct {
	inner upstream.Client
}

func (f *listFailUpstream) ID() string     { return f.inner.ID() }
func (f *listFailUpstream) Prefix() string { return f.inner.Prefix() }
func (f *listFailUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		return nil, errors.New("list down")
	}
	return f.inner.Call(ctx, req)
}

type errReplyUpstream struct {
	inner *mock.MockUpstream
}

func (e *errReplyUpstream) ID() string     { return e.inner.ID() }
func (e *errReplyUpstream) Prefix() string { return e.inner.Prefix() }
func (e *errReplyUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/call" {
		return rpc.NewError(req.ID, 99, "upstream says no", nil), nil
	}
	return e.inner.Call(ctx, req)
}

func TestInitializeMergeTwoUpstreams(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New(context.Background(), []upstream.Client{b1, b2}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var merged map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &merged))
	extras := merged["serverInfo"].(map[string]any)["extras"].(map[string]any)
	upstreams := extras["backends"].([]any)
	require.Len(t, upstreams, 2)
}

func TestInitializeOmitsFailedUpstream(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New(context.Background(), []upstream.Client{
		b1,
		&initFailUpstream{inner: b2},
	}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var merged map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &merged))
	extras := merged["serverInfo"].(map[string]any)["extras"].(map[string]any)
	upstreams := extras["backends"].([]any)
	require.Len(t, upstreams, 1)
	require.Equal(t, "b1", upstreams[0])
}

func TestInitializeAllUpstreamsFail(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New(context.Background(), []upstream.Client{
		&initFailUpstream{inner: b1},
		&initFailUpstream{inner: b2},
	}, WithListTTL(0))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`7`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
	require.JSONEq(t, `7`, string(resp.ID))
}

func TestInitializeAllFailReportsPartialFailuresWhenEnabled(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New(context.Background(), []upstream.Client{
		&initFailUpstream{inner: b1},
		&initFailUpstream{inner: b2},
	}, WithListTTL(0), WithReportPartialFailures(true))
	require.NoError(t, err)

	resp, err := a.Initialize(context.Background(), json.RawMessage(`8`))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.NotNil(t, resp.Error.Data)
	var data struct {
		PartialFailures []struct {
			UpstreamID string `json:"backend_id"`
		} `json:"partial_failures"`
	}
	require.NoError(t, json.Unmarshal(resp.Error.Data, &data))
	require.Len(t, data.PartialFailures, 2)
}

func TestToolsListOmitsFailedUpstream(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("b2", "beta", []string{"ping"})
	a, err := New(context.Background(), []upstream.Client{
		b1,
		&listFailUpstream{inner: b2},
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
	a, err := New(context.Background(), []upstream.Client{z, ap}, WithListTTL(0))
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
	a, err := New(context.Background(), []upstream.Client{b1, b2}, WithListTTL(0))
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

func TestToolsCallForwardsUpstreamJSONRPCErrorPreservesID(t *testing.T) {
	good := mock.NewMockUpstream("b1", "p1", []string{"echo"})
	errBack := &errReplyUpstream{inner: mock.NewMockUpstream("b2", "p2", []string{"ping"})}
	a, err := New(context.Background(), []upstream.Client{good, errBack}, WithListTTL(0))
	require.NoError(t, err)

	params := map[string]any{"name": "p2__ping", "arguments": map[string]any{}}
	pb, _ := json.Marshal(params)

	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`42`), pb)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, 99, resp.Error.Code)
	require.JSONEq(t, `42`, string(resp.ID))
}
