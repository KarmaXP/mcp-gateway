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

func TestCallSurvivesReconnectWhenPOSTInFlight(t *testing.T) {
	var (
		mu           sync.Mutex
		sessID       string
		sseCount     atomic.Int32
		events       chan string
		dropFirstSSE = make(chan struct{})
		sendResponse = make(chan struct{})
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/sse", func(w http.ResponseWriter, r *http.Request) {
		n := int(sseCount.Add(1))
		sid := fmt.Sprintf("sess-%d", n)
		mu.Lock()
		sessID = sid
		events = make(chan string, 4)
		mu.Unlock()

		w.Header().Set(mcpwire.HeaderMCPSessionID, sid)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, ok := w.(http.Flusher)
		require.True(t, ok)
		fl.Flush()

		if n == 1 {
			<-dropFirstSSE
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
		w.WriteHeader(http.StatusAccepted)
		go func() {
			<-sendResponse
			result, err := json.Marshal(map[string]any{"ok": true})
			require.NoError(t, err)
			resp := rpc.NewResult(req.ID, result)
			raw, err := json.Marshal(resp)
			require.NoError(t, err)
			mu.Lock()
			ch := events
			mu.Unlock()
			if ch != nil {
				ch <- string(raw)
			}
		}()
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	require.NoError(t, c.ensureSession(context.Background()))
	require.Equal(t, int32(1), sseCount.Load())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	callDone := make(chan struct {
		resp *rpc.Response
		err  error
	}, 1)
	go func() {
		resp, err := c.Call(ctx, &rpc.Request{
			JSONRPC: rpc.JSONRPCVersion,
			ID:      json.RawMessage(`55`),
			Method:  "tools/list",
		})
		callDone <- struct {
			resp *rpc.Response
			err  error
		}{resp, err}
	}()

	require.Eventually(t, func() bool {
		c.pendMu.Lock()
		defer c.pendMu.Unlock()
		_, ok := c.pending["55"]
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	close(dropFirstSSE)

	require.Eventually(t, func() bool {
		return sseCount.Load() >= 2
	}, 2*time.Second, 10*time.Millisecond)

	close(sendResponse)

	select {
	case out := <-callDone:
		require.NoError(t, out.err)
		require.NotNil(t, out.resp)
		require.Contains(t, string(out.resp.Result), "ok")
		require.GreaterOrEqual(t, sseCount.Load(), int32(2))
	case <-time.After(3 * time.Second):
		t.Fatal("Call did not complete after reconnect with POST in flight")
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

func TestDispatchResponseAbortsPendingWhenChannelFull(t *testing.T) {
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

	c.pendMu.Lock()
	deliverErr := c.pendingErr["9"]
	c.pendMu.Unlock()
	require.Error(t, deliverErr)
	require.Contains(t, deliverErr.Error(), "pending channel full")

	_, ok := <-ch
	require.True(t, ok)
	_, ok = <-ch
	require.False(t, ok)
}

func TestParallelEnsureSessionUsesSingleConnect(t *testing.T) {
	var (
		sseCount     atomic.Int32
		firstConnect = make(chan struct{})
		release      = make(chan struct{})
		firstOnce    sync.Once
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != mcpwire.PathMCPSSE {
			http.NotFound(w, r)
			return
		}
		sseCount.Add(1)
		firstOnce.Do(func() { close(firstConnect) })
		w.Header().Set(mcpwire.HeaderMCPSessionID, "parallel-sess")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	const workers = 12
	begin := make(chan struct{})
	errs := make([]error, workers)
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-begin
			errs[i] = c.ensureSession(context.Background())
		}(i)
	}
	ready.Wait()
	close(begin)

	select {
	case <-firstConnect:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE connect")
	}
	require.Equal(t, int32(1), sseCount.Load())

	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "worker %d", i)
	}
	require.Equal(t, int32(1), sseCount.Load())
	close(release)
}

func TestCallFailsFastOnSSEDisconnect(t *testing.T) {
	dropSSE := make(chan struct{})
	var postCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == mcpwire.PathMCPSSE:
			w.Header().Set(mcpwire.HeaderMCPSessionID, "drop-sess")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			select {
			case <-dropSSE:
			case <-r.Context().Done():
			}
		case r.Method == http.MethodPost && r.URL.Path == mcpwire.PathMCPRPC:
			if postCount.Add(1) > 1 {
				http.Error(w, "session lost", http.StatusGone)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	require.NoError(t, c.ensureSession(context.Background()))

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := c.Call(callCtx, &rpc.Request{
			JSONRPC: rpc.JSONRPCVersion,
			ID:      json.RawMessage(`7`),
			Method:  "tools/list",
		})
		callDone <- err
	}()

	require.Eventually(t, func() bool {
		c.pendMu.Lock()
		defer c.pendMu.Unlock()
		_, ok := c.pending["7"]
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	start := time.Now()
	close(dropSSE)

	select {
	case err := <-callDone:
		require.Error(t, err)
		require.True(t,
			strings.Contains(err.Error(), "sse stream ended") ||
				strings.Contains(err.Error(), "session lost"),
			"unexpected error: %v", err)
		require.Less(t, time.Since(start), 2*time.Second)
	case <-time.After(3 * time.Second):
		t.Fatal("Call did not fail fast after SSE disconnect")
	}
}

func TestConnectRequestContextCancelsWhenEitherParentDone(t *testing.T) {
	t.Parallel()
	lifecycle, stopLifecycle := context.WithCancel(context.Background())
	defer stopLifecycle()
	callCtx, stopCall := context.WithCancel(context.Background())
	defer stopCall()

	merged := connectRequestContext(lifecycle, callCtx)
	require.NoError(t, merged.Err())

	stopCall()
	require.Eventually(t, func() bool {
		return merged.Err() != nil
	}, time.Second, 5*time.Millisecond)

	merged2 := connectRequestContext(lifecycle, callCtx)
	stopLifecycle()
	require.Eventually(t, func() bool {
		return merged2.Err() != nil
	}, time.Second, 5*time.Millisecond)
}

func TestEnsureSessionRespectsCallContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != mcpwire.PathMCPSSE {
			http.NotFound(w, r)
			return
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = c.ensureSession(ctx)
	elapsed := time.Since(start)
	require.Error(t, err)
	require.Less(t, elapsed, 500*time.Millisecond)
}

func TestCallRejectsDuplicateJSONRPCID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == mcpwire.PathMCPSSE {
			w.Header().Set(mcpwire.HeaderMCPSessionID, "dup-sess")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, c.ensureSession(context.Background()))

	ch := make(chan *rpc.Response, pendingJSONRPCChannelCap)
	c.pendMu.Lock()
	c.pending["99"] = ch
	c.pendMu.Unlock()

	_, err = c.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		ID:      json.RawMessage(`99`),
		Method:  "tools/list",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate jsonrpc id")
}

func TestCallReturnsErrorWhenPendingChannelFull(t *testing.T) {
	blockPOST := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == mcpwire.PathMCPSSE:
			w.Header().Set(mcpwire.HeaderMCPSessionID, "full-sess")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case r.Method == http.MethodPost && r.URL.Path == mcpwire.PathMCPRPC:
			<-blockPOST
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c, cleanup, err := NewHTTPMCPUpstream(context.Background(), "u1", "alpha", srv.URL, 1, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	require.NoError(t, c.ensureSession(context.Background()))

	req := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		ID:      json.RawMessage(`11`),
		Method:  "tools/list",
	}
	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()

	callDone := make(chan error, 1)
	go func() {
		_, err := c.Call(callCtx, req)
		callDone <- err
	}()

	require.Eventually(t, func() bool {
		c.pendMu.Lock()
		defer c.pendMu.Unlock()
		_, ok := c.pending["11"]
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	c.pendMu.Lock()
	ch := c.pending["11"]
	c.pendMu.Unlock()
	require.NotNil(t, ch)
	ch <- &rpc.Response{JSONRPC: rpc.JSONRPCVersion, ID: json.RawMessage(`11`), Result: json.RawMessage(`{}`)}

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      11,
		"result":  map[string]any{"ok": true},
	})
	require.NoError(t, err)
	c.dispatch(raw)
	close(blockPOST)

	select {
	case err := <-callDone:
		require.Error(t, err)
		require.Contains(t, err.Error(), "pending channel full")
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after pending channel full")
	}
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
