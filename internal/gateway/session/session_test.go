package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestSessionToolsListBeforeHandshake(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)

	s := New(context.Background(), "test-session", agg, nil)
	req := &rpc.Request{
		JSONRPC: rpc.Version,
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
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)

	s := New(context.Background(), "test-session", agg, nil)
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.Version, Method: "notifications/initialized"}))

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
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
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)

	mw := Middleware(func(ctx context.Context, req *rpc.Request) error {
		if req.Method == "tools/list" {
			return context.DeadlineExceeded // any sentinel error text
		}
		return nil
	})
	s := New(context.Background(), "test-session", agg, []Middleware{mw})

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.Version, Method: "initialize", ID: json.RawMessage(`0`), Params: json.RawMessage(`{}`)}))
	<-s.Out()

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.Version, Method: "notifications/initialized"}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.Version, Method: "tools/list", ID: json.RawMessage(`3`)}))

	raw := <-s.Out()
	var resp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, errcodes.RequestRejected, resp.Error.Code)
}

func TestSessionFullHandshakeAndToolsList(t *testing.T) {
	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)

	s := New(context.Background(), "test-session", agg, nil)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{
		JSONRPC: rpc.Version,
		Method:  "initialize",
		ID:      json.RawMessage(`1`),
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}`),
	}))
	raw := <-s.Out()
	var initResp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &initResp))
	require.Nil(t, initResp.Error)

	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.Version, Method: "notifications/initialized"}))
	require.NoError(t, s.Dispatch(context.Background(), &rpc.Request{JSONRPC: rpc.Version, Method: "tools/list", ID: json.RawMessage(`2`)}))

	raw = <-s.Out()
	var listResp rpc.Response
	require.NoError(t, json.Unmarshal(raw, &listResp))
	require.Nil(t, listResp.Error)
	require.Contains(t, string(listResp.Result), "alpha__echo")
}
