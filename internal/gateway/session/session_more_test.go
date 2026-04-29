package session

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestManagerGetRemove(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	m := NewManager(agg)
	s := m.Create(context.Background())
	got, err := m.Get(s.ID())
	require.NoError(t, err)
	require.Equal(t, s, got)
	m.Remove(s.ID())
	_, err = m.Get(s.ID())
	require.Error(t, err)
}

func TestSessionMethodNotFound(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "s1", agg, nil)
	handshake(t, s)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "resources/list",
		ID:      json.RawMessage(`9`),
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.MethodNotFound, resp.Error.Code)
}

func TestSessionPingReturnsEmptyResult(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "ping-session", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "ping",
		ID:      json.RawMessage(`42`),
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Nil(t, resp.Error)
	require.JSONEq(t, `42`, string(resp.ID))
	require.JSONEq(t, `{}`, string(resp.Result))
}

func TestSessionLegacyInitializedNotification(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "s2", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "initialized",
	}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "tools/list",
		ID:      json.RawMessage(`2`),
	}))
	raw := <-s.Out()
	var list rpc.Response
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Nil(t, list.Error)
}

func TestSessionMiddlewareNilSkipped(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "s3", agg, []Middleware{nil})
	handshake(t, s)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "tools/list",
		ID:      json.RawMessage(`3`),
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Nil(t, resp.Error)
}

func TestSessionMiddlewareRejectsNotification(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	mw := Middleware(func(context.Context, *rpc.Request) error {
		return fmt.Errorf("blocked")
	})
	s := New(context.Background(), "s4", agg, []Middleware{mw})
	err = s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "notifications/initialized",
	})
	require.Error(t, err)
}

func TestUnknownNotificationIgnored(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "s5", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "notifications/custom",
	}))
	select {
	case <-s.Out():
		t.Fatal("unexpected outbound message")
	case <-time.After(50 * time.Millisecond):
	}
}

func handshake(t *testing.T, s *Session) {
	t.Helper()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "notifications/initialized",
	}))
}

func TestSessionToolsCallAfterHandshake(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "s7", agg, nil)
	handshake(t, s)
	params, _ := json.Marshal(map[string]any{
		"name":      "alpha__echo",
		"arguments": map[string]any{"msg": "hi"},
	})
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "tools/call",
		ID:      json.RawMessage(`4`),
		Params:  params,
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Nil(t, resp.Error)
	require.Contains(t, string(resp.Result), "ok from b1:echo")
}

func TestSessionDispatchNilRequestContext(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)
	s := New(context.Background(), "s6", agg, nil)
	require.NoError(t, s.Dispatch(nil, &rpc.Request{ //nolint:staticcheck // nil exercises mergedCancel fallback
		JSONRPC: rpc.Version,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	select {
	case <-s.Out():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
