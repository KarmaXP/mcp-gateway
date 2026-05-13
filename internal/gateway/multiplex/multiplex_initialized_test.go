package multiplex

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type recordingUpstream struct {
	inner backend.Upstream

	mu      sync.Mutex
	methods []string
}

func newRecordingUpstream(inner backend.Upstream) *recordingUpstream {
	return &recordingUpstream{inner: inner}
}

func (r *recordingUpstream) ID() string     { return r.inner.ID() }
func (r *recordingUpstream) Prefix() string { return r.inner.Prefix() }

func (r *recordingUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	r.mu.Lock()
	r.methods = append(r.methods, req.Method)
	r.mu.Unlock()
	return r.inner.Call(ctx, req)
}

func (r *recordingUpstream) Methods() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.methods...)
}

type handshakeAwareBackend struct {
	inner backend.Upstream

	mu                  sync.Mutex
	initializedNotified bool
}

func newHandshakeAwareBackend(inner backend.Upstream) *handshakeAwareBackend {
	return &handshakeAwareBackend{inner: inner}
}

func (b *handshakeAwareBackend) ID() string     { return b.inner.ID() }
func (b *handshakeAwareBackend) Prefix() string { return b.inner.Prefix() }

func (b *handshakeAwareBackend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.IsNotification() && (req.Method == "notifications/initialized" || req.Method == "initialized") {
		b.mu.Lock()
		b.initializedNotified = true
		b.mu.Unlock()
		return nil, nil
	}
	if req.Method == "tools/list" {
		b.mu.Lock()
		ready := b.initializedNotified
		b.mu.Unlock()
		if !ready {
			return rpc.NewError(req.ID, errcodes.HandshakeIncomplete, "upstream handshake incomplete", nil), nil
		}
	}
	return b.inner.Call(ctx, req)
}

func TestNotifyHostInitializedFansOutToAllUpstreams(t *testing.T) {
	rec1 := newRecordingUpstream(mock.NewMockUpstream("b1", "alpha", []string{"echo"}))
	rec2 := newRecordingUpstream(mock.NewMockUpstream("b2", "beta", []string{"ping"}))
	mpx, err := New([]backend.Upstream{rec1, rec2}, WithListTTL(0))
	require.NoError(t, err)

	_, err = mpx.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	mpx.NotifyHostInitialized(context.Background())

	for _, rec := range []*recordingUpstream{rec1, rec2} {
		methods := rec.Methods()
		require.Contains(t, methods, "initialize")
		require.Contains(t, methods, "notifications/initialized")
		initIdx := indexOf(methods, "initialize")
		notifyIdx := indexOf(methods, "notifications/initialized")
		require.Greater(t, notifyIdx, initIdx, "notifications/initialized must follow initialize for %s", rec.ID())
	}
}

func TestNotifyHostInitializedUnlocksUpstreamToolsList(t *testing.T) {
	inner := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	up := newHandshakeAwareBackend(inner)
	mpx, err := New([]backend.Upstream{up}, WithListTTL(0))
	require.NoError(t, err)

	_, err = mpx.Initialize(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)

	blocked, err := mpx.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Nil(t, blocked.Error)
	require.NotContains(t, string(blocked.Result), "alpha__echo")

	mpx.NotifyHostInitialized(context.Background())

	allowed, err := mpx.ToolsList(context.Background(), json.RawMessage(`3`))
	require.NoError(t, err)
	require.Nil(t, allowed.Error)
	require.Contains(t, string(allowed.Result), "alpha__echo")
}

func indexOf(methods []string, method string) int {
	for i, m := range methods {
		if m == method {
			return i
		}
	}
	return -1
}
