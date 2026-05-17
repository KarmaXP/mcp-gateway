package mcphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestDispatchNotificationInvokesCallback(t *testing.T) {
	t.Parallel()
	methods := []string{
		mcpwire.NotificationToolsListChanged,
		mcpwire.LegacyToolsListChanged,
		mcpwire.NotificationResourcesListChanged,
		mcpwire.LegacyResourcesListChanged,
		mcpwire.NotificationPromptsListChanged,
		mcpwire.LegacyPromptsListChanged,
	}
	for _, method := range methods {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", "http://example.invalid", 1, "")
			require.NoError(t, err)
			defer cleanup()

			var (
				mu   sync.Mutex
				seen *rpc.Request
			)
			c.SetOnNotification(func(req *rpc.Request) {
				mu.Lock()
				seen = req
				mu.Unlock()
			})

			raw, err := json.Marshal(map[string]any{
				"jsonrpc": rpc.JSONRPCVersion,
				"method":  method,
			})
			require.NoError(t, err)
			c.dispatch(raw)

			mu.Lock()
			defer mu.Unlock()
			require.NotNil(t, seen)
			require.Equal(t, method, seen.Method)
			require.True(t, seen.IsNotification())
		})
	}
}

func TestDispatchResponseDoesNotInvokeCallback(t *testing.T) {
	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", "http://example.invalid", 1, "")
	require.NoError(t, err)
	defer cleanup()

	called := false
	c.SetOnNotification(func(req *rpc.Request) {
		called = true
	})

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      1,
		"result":  map[string]any{},
	})
	require.NoError(t, err)
	c.dispatch(raw)
	require.False(t, called)
}

func TestCloseDoesNotHangOnBlockedSSEReader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != mcpwire.PathMCPSSE {
			http.NotFound(w, r)
			return
		}
		w.Header().Set(mcpwire.HeaderMCPSessionID, "blocked-sess")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, ok := w.(http.Flusher)
		require.True(t, ok)
		_, _ = fmt.Fprint(w, ":\n\n")
		fl.Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)

	require.NoError(t, c.ensureSession(context.Background()))
	require.True(t, c.connected)

	done := make(chan struct{})
	go func() {
		cleanup()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("close hung while SSE reader blocked")
	}
}

func TestReconnectAfterSSEDisconnect(t *testing.T) {

	var (
		mu       sync.Mutex
		sessID   string
		sseCount atomic.Int32
		events   = make(chan string, 8)
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/sse", func(w http.ResponseWriter, r *http.Request) {
		n := int(sseCount.Add(1))
		sid := fmt.Sprintf("sess-%d", n)
		mu.Lock()
		sessID = sid
		mu.Unlock()

		w.Header().Set(mcpwire.HeaderMCPSessionID, sid)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, ok := w.(http.Flusher)
		require.True(t, ok)
		fl.Flush()

		if n == 1 {
			return
		}

		for {
			select {
			case <-r.Context().Done():
				return
			case msg, ok := <-events:
				if !ok {
					return
				}
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", mcpwire.SSEJSONRPCEvent, msg)
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("POST /mcp/rpc", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		want := sessID
		mu.Unlock()
		if got := r.Header.Get(mcpwire.HeaderMCPSessionID); got != want || want == "" {
			http.Error(w, "bad session", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		req, err := rpc.ParseRequest(body)
		if err != nil {
			http.Error(w, "parse", http.StatusBadRequest)
			return
		}
		if req.IsNotification() {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result, err := json.Marshal(map[string]any{"ok": true})
		require.NoError(t, err)
		resp := rpc.NewResult(req.ID, result)
		raw, err := json.Marshal(resp)
		require.NoError(t, err)
		select {
		case events <- string(raw):
		default:
			http.Error(w, "queue full", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	require.NoError(t, c.ensureSession(context.Background()))
	require.Equal(t, int32(1), sseCount.Load())

	require.Eventually(t, func() bool {
		c.connMu.Lock()
		defer c.connMu.Unlock()
		return !c.connected && c.connErr != nil
	}, 2*time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.Call(ctx, &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		ID:      json.RawMessage(`42`),
		Method:  "tools/list",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.GreaterOrEqual(t, sseCount.Load(), int32(2))
	require.Contains(t, string(resp.Result), "ok")
}

func TestDispatchResponseCountsDropWhenChannelFull(t *testing.T) {
	prev := responseDeliverTimeout
	responseDeliverTimeout = 20 * time.Millisecond
	t.Cleanup(func() { responseDeliverTimeout = prev })

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", "http://example.invalid", 1, "")
	require.NoError(t, err)
	defer cleanup()

	ch := make(chan *rpc.Response, pendingJSONRPCChannelCap)
	c.pendMu.Lock()
	c.pending["9"] = ch
	c.pendMu.Unlock()
	ch <- &rpc.Response{JSONRPC: rpc.JSONRPCVersion, ID: json.RawMessage(`9`), Result: json.RawMessage(`{}`)}

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      9,
		"result":  map[string]any{},
	})
	require.NoError(t, err)
	c.dispatch(raw)
	require.Equal(t, uint64(1), c.DroppedResponses())
}

func TestRPCClientHasTimeout(t *testing.T) {
	t.Parallel()

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", "http://example.invalid", 1, "")
	require.NoError(t, err)
	defer cleanup()

	require.NotZero(t, c.rpcClient.Timeout)
	require.Zero(t, c.sseClient.Timeout)
}

func TestEnsureSessionUsesBearerToken(t *testing.T) {
	t.Parallel()

	const token = "secret-token"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == mcpwire.PathMCPSSE {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set(mcpwire.HeaderMCPSessionID, "s1")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, token)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	err = c.ensureSession(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer "+token, gotAuth)
	require.True(t, strings.HasPrefix(gotAuth, "Bearer "))
}
