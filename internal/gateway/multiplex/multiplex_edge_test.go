package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type flakyUpstream struct {
	*mock.MockUpstream
	mu         sync.Mutex
	listCalls  int
	failFirstN int
	listErr    error
}

func newFlakyUpstream(inner *mock.MockUpstream, failFirst int, err error) *flakyUpstream {
	return &flakyUpstream{MockUpstream: inner, failFirstN: failFirst, listErr: err}
}

func (f *flakyUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/list" {
		f.mu.Lock()
		f.listCalls++
		n := f.listCalls
		failN := f.failFirstN
		err := f.listErr
		f.mu.Unlock()
		if n <= failN && err != nil {
			return nil, err
		}
	}
	return f.MockUpstream.Call(ctx, req)
}

func (f *flakyUpstream) ID() string     { return f.MockUpstream.ID() }
func (f *flakyUpstream) Prefix() string { return f.MockUpstream.Prefix() }

var _ upstream.Client = (*flakyUpstream)(nil)

func TestToolsListPartialUpstreamFailureOmitsTools(t *testing.T) {
	ok := mock.NewMockUpstream("ok", "alpha", []string{"echo"})
	bad := newFlakyUpstream(mock.NewMockUpstream("bad", "beta", []string{"ping"}), 1, errors.New("upstream down"))

	a, err := New(context.Background(), []upstream.Client{ok, bad}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`0`))

	resp, err := a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.Contains(t, string(resp.Result), "alpha__echo")
	require.NotContains(t, string(resp.Result), "beta__ping")
}

func TestToolsCallTimeoutWhenUpstreamSlow(t *testing.T) {
	slow := mock.NewMockUpstream("s", "alpha", []string{"echo"})
	slow.ToolsCallDelay = 500 * time.Millisecond

	a, err := New(context.Background(), []upstream.Client{slow}, WithListTTL(0), WithCallTimeout(50*time.Millisecond))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`0`))

	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`9`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}

func TestToolsCallUpstreamTransportError(t *testing.T) {
	b := mock.NewMockUpstream("s", "alpha", []string{"echo"})
	b.ToolsCallErr = errors.New("connection reset")

	a, err := New(context.Background(), []upstream.Client{b}, WithListTTL(0))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`0`))

	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(context.Background(), json.RawMessage(`9`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
}
