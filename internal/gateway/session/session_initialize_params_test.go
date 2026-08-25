package session

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

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

func TestSessionInitializeForwardsHostParamsToUpstream(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	rec := &initParamsRecorder{inner: inner}
	mpx, err := multiplex.New(context.Background(), []backend.Upstream{rec}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	s := NewSession(context.Background(), "sess-init-params", mpx, nil)
	hostParams := json.RawMessage(`{
		"protocolVersion":"2099-01-01",
		"capabilities":{"experimental":{}},
		"clientInfo":{"name":"custom-host","version":"9.9.9"}
	}`)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  hostParams,
	}))

	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.Nil(t, resp.Error)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initialize response")
	}

	rec.mu.Lock()
	got := append(json.RawMessage(nil), rec.params...)
	rec.mu.Unlock()
	var gotMap map[string]any
	require.NoError(t, json.Unmarshal(got, &gotMap))
	require.Equal(t, "2099-01-01", gotMap["protocolVersion"])
	clientInfo, ok := gotMap["clientInfo"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "custom-host", clientInfo["name"])
}
