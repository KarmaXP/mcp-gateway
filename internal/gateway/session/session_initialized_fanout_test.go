package session

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type sessionRecordingUpstream struct {
	inner backend.Upstream

	mu      sync.Mutex
	methods []string
}

func newSessionRecordingUpstream(inner backend.Upstream) *sessionRecordingUpstream {
	return &sessionRecordingUpstream{inner: inner}
}

func (r *sessionRecordingUpstream) ID() string     { return r.inner.ID() }
func (r *sessionRecordingUpstream) Prefix() string { return r.inner.Prefix() }

func (r *sessionRecordingUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	r.mu.Lock()
	r.methods = append(r.methods, req.Method)
	r.mu.Unlock()
	return r.inner.Call(ctx, req)
}

func (r *sessionRecordingUpstream) Methods() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.methods...)
}

func TestSessionHostInitializedFansOutToUpstreams(t *testing.T) {
	rec := newSessionRecordingUpstream(mock.NewMockUpstream("b1", "alpha", []string{"echo"}))
	agg, err := multiplex.New([]backend.Upstream{rec}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	s := NewSession(context.Background(), "fanout-session", agg, nil)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()

	require.Equal(t, []string{"initialize"}, rec.Methods())

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "notifications/initialized",
	}))

	methods := rec.Methods()
	require.Equal(t, []string{"initialize", "notifications/initialized"}, methods)
}
