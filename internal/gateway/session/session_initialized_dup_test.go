package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestDuplicateInitializedNotificationNotifiesUpstreamOnce(t *testing.T) {
	rec := newRecordingUpstream(mock.NewMockUpstream("b1", "alpha", []string{"echo"}))
	mpx, err := multiplex.New([]backend.Upstream{rec}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	sm := NewSessionManager(mpx)
	sess := sm.Create(context.Background())

	initReq := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}`),
	}
	require.NoError(t, sess.Dispatch(context.Background(), initReq))

	notify := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}
	require.NoError(t, sess.Dispatch(context.Background(), notify))
	require.NoError(t, sess.Dispatch(context.Background(), notify))

	count := 0
	for _, m := range rec.Methods() {
		if m == "notifications/initialized" {
			count++
		}
	}
	require.Equal(t, 1, count)
}

type recordingUpstream struct {
	inner *mock.MockUpstream

	methods []string
}

func newRecordingUpstream(inner *mock.MockUpstream) *recordingUpstream {
	return &recordingUpstream{inner: inner}
}

func (r *recordingUpstream) ID() string     { return r.inner.ID() }
func (r *recordingUpstream) Prefix() string { return r.inner.Prefix() }

func (r *recordingUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	r.methods = append(r.methods, req.Method)
	return r.inner.Call(ctx, req)
}

func (r *recordingUpstream) Methods() []string {
	return append([]string(nil), r.methods...)
}
