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
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestSessionToolsListBeforeHandshake(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	s := NewSession(context.Background(), "test-session", agg, nil)
	req := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`1`),
	}
	require.NoError(t, s.Dispatch(context.Background(), req))

	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, errcodes.HandshakeIncomplete, resp.Error.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE payload")
	}
}

func TestSessionInitializedNotificationWithoutInitializeDoesNotOpenTools(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	s := NewSession(context.Background(), "test-session", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}))

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`2`),
	}))
	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, errcodes.HandshakeIncomplete, resp.Error.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSessionMiddlewareRejectsWithRequestRejected(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	mw := Middleware(func(ctx context.Context, req *rpc.Request) error {
		if req.Method == "tools/list" {
			return context.DeadlineExceeded // any sentinel error text
		}
		return nil
	})
	s := NewSession(context.Background(), "test-session", agg, []Middleware{mw})

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "initialize", ID: json.RawMessage(`0`), Params: json.RawMessage(`{}`)}))
	<-s.Out()

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "tools/list", ID: json.RawMessage(`3`)}))

	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.RequestRejected, resp.Error.Code)
}

func TestSessionFullHandshakeAndToolsList(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	s := NewSession(context.Background(), "test-session", agg, nil)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	raw := <-s.Out()
	var initResp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &initResp))
	require.Nil(t, initResp.Error)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "tools/list", ID: json.RawMessage(`2`)}))

	raw = <-s.Out()
	var listResp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &listResp))
	require.Nil(t, listResp.Error)
	require.Contains(t, string(listResp.Result), "alpha__echo")
}

func TestSessionManagerGetRemove(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	m := NewSessionManager(agg)
	s := m.Create(context.Background())
	got, err := m.Get(s.ID())
	require.NoError(t, err)
	require.Equal(t, s, got)
	m.Remove(s.ID())
	_, err = m.Get(s.ID())
	require.Error(t, err)
}

func TestSessionMethodNotFound(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s1", agg, nil)
	handshake(t, s)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "experimental/unsupported_method",
		ID:      json.RawMessage(`9`),
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.MethodNotFound, resp.Error.Code)
}

func TestSessionPingReturnsEmptyResult(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "ping-session", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
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
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s2", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialized",
	}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`2`),
	}))
	raw := <-s.Out()
	var list rpc.Response
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Nil(t, list.Error)
}

func TestSessionMiddlewareNilSkipped(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s3", agg, []Middleware{nil})
	handshake(t, s)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`3`),
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Nil(t, resp.Error)
}

func TestSessionMiddlewareRejectsNotification(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	mw := Middleware(func(context.Context, *rpc.Request) error {
		return fmt.Errorf("blocked")
	})
	s := NewSession(context.Background(), "s4", agg, []Middleware{mw})
	err = s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "notifications/initialized",
	})
	require.Error(t, err)
}

func TestUnknownNotificationIgnored(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s5", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
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
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "notifications/initialized",
	}))
}

func TestSessionToolsCallAfterHandshake(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s7", agg, nil)
	handshake(t, s)
	params, _ := json.Marshal(map[string]any{
		"name":      "alpha__echo",
		"arguments": map[string]any{"msg": "hi"},
	})
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
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

func TestSessionResourcesListEmptyWhenBackendsOmit(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s-res", agg, nil)
	handshake(t, s)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "resources/list",
		ID:      json.RawMessage(`8`),
	}))
	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.Nil(t, resp.Error)
	var body struct {
		Resources []any `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &body))
	require.Len(t, body.Resources, 0)
}

func TestSessionToolHistoryRecordsSuccessfulToolsCall(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s-hist", agg, nil)
	handshake(t, s)
	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      json.RawMessage(`4`),
		Params:  params,
	}))
	<-s.Out()
	hist := s.recentToolSnapshot()
	require.Equal(t, []string{"alpha__echo"}, hist)
}

func TestSessionDispatchNilRequestContext(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	s := NewSession(context.Background(), "s6", agg, nil)
	require.NoError(t, s.Dispatch(nil, &rpc.Request{ //nolint:staticcheck // nil exercises mergedCancel fallback
		JSONRPC: rpc.JSONRPCVersion,
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

func TestSessionMiddlewareRejectionDoesNotAlsoDispatch(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	mw := Middleware(func(ctx context.Context, req *rpc.Request) error {
		if req.Method == "tools/list" {
			return context.DeadlineExceeded
		}
		return nil
	})
	s := NewSession(context.Background(), "reject-once", agg, []Middleware{mw})

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "initialize", ID: json.RawMessage(`0`), Params: json.RawMessage(`{}`)}))
	<-s.Out()
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "notifications/initialized"}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "tools/list", ID: json.RawMessage(`3`)}))

	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.RequestRejected, resp.Error.Code)

	select {
	case extra := <-s.Out():
		t.Fatalf("a rejected request must not run: the host got a second payload for id 3: %s", extra)
	case <-time.After(500 * time.Millisecond):
	}
}
