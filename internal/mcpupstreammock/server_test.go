package mcpupstreammock

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToolsListAndCallEcho(t *testing.T) {
	cfg := Config{
		ListenAddr: "127.0.0.1:0",
		ServerName: "test-mock",
		Tools: []Tool{{
			Name:        "echo",
			Description: "echo",
			CallText:    "alpha-ok",
		}},
	}
	s := &server{cfg: cfg, events: make(chan string, 8)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/sse", s.handleSSE)
	mux.HandleFunc("POST /mcp/rpc", s.handleRPC)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	sseResp, err := http.Get(ts.URL + "/mcp/sse")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sseResp.Body.Close() })

	var sseMu sync.Mutex
	var sseBuf strings.Builder
	go func() {
		reader := bufio.NewReader(sseResp.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			sseMu.Lock()
			sseBuf.WriteString(line)
			sseMu.Unlock()
		}
	}()

	require.Equal(t, http.StatusOK, sseResp.StatusCode)
	sid := sseResp.Header.Get("Mcp-Session-Id")
	require.NotEmpty(t, sid)

	post := func(body string) {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/mcp/rpc", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Mcp-Session-Id", sid)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, resp.StatusCode)
		_ = resp.Body.Close()
	}

	sseContains := func(needle string) bool {
		sseMu.Lock()
		defer sseMu.Unlock()
		return strings.Contains(sseBuf.String(), needle)
	}

	post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	require.Eventually(t, func() bool { return sseContains(`"id":1`) && sseContains(`"result"`) }, 2*time.Second, 20*time.Millisecond)

	post(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Eventually(t, func() bool { return sseContains(`"id":2`) && sseContains(`echo`) }, 2*time.Second, 20*time.Millisecond)

	post(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	require.Eventually(t, func() bool { return sseContains(`"id":3`) && sseContains(`alpha-ok`) }, 2*time.Second, 20*time.Millisecond)
}

func TestCallTextUnknownTool(t *testing.T) {
	s := &server{cfg: Config{Tools: []Tool{{Name: "echo", CallText: "ok"}}}}
	require.Equal(t, "ok", s.callText("echo"))
	require.Equal(t, "unknown-tool", s.callText("missing"))
}

func TestCallTextDefaultSuffix(t *testing.T) {
	s := &server{cfg: Config{Tools: []Tool{{Name: "ping"}}}}
	require.Equal(t, "ping-ok", s.callText("ping"))
}

func TestRun_EmptyListenAddr(t *testing.T) {
	require.Error(t, Run(Config{}))
}

func TestBadSession(t *testing.T) {
	ts, _, _ := startMockHTTPServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	postRPC(t, ts.URL, "wrong-session", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, http.StatusUnauthorized)
}

func TestInvalidRPCBody(t *testing.T) {
	ts, sid, _ := startMockHTTPServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	postRPC(t, ts.URL, sid, `{not json`, http.StatusBadRequest)
}

func TestUnknownMethod(t *testing.T) {
	ts, sid, sseBody := startMockHTTPServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	postRPC(t, ts.URL, sid, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`, http.StatusAccepted)
	require.Eventually(t, func() bool { return strings.Contains(sseBody(), `"id":1`) }, 2*time.Second, 20*time.Millisecond)

	postRPC(t, ts.URL, sid, `{"jsonrpc":"2.0","id":9,"method":"ping/unknown"}`, http.StatusAccepted)
	require.Eventually(t, func() bool {
		body := sseBody()
		return strings.Contains(body, `"id":9`) && strings.Contains(body, "not found")
	}, 2*time.Second, 20*time.Millisecond)
}

func startMockHTTPServer(t *testing.T, cfg Config) (*httptest.Server, string, func() string) {
	t.Helper()
	s := &server{cfg: cfg, events: make(chan string, 8)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp/sse", s.handleSSE)
	mux.HandleFunc("POST /mcp/rpc", s.handleRPC)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	sseResp, err := http.Get(ts.URL + "/mcp/sse")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sseResp.Body.Close() })
	sid := sseResp.Header.Get("Mcp-Session-Id")
	require.NotEmpty(t, sid)

	var sseMu sync.Mutex
	var sseBuf strings.Builder
	go func() {
		reader := bufio.NewReader(sseResp.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			sseMu.Lock()
			sseBuf.WriteString(line)
			sseMu.Unlock()
		}
	}()
	sseBody := func() string {
		sseMu.Lock()
		defer sseMu.Unlock()
		return sseBuf.String()
	}
	return ts, sid, sseBody
}

func postRPC(t *testing.T, baseURL, sessionID, body string, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp/rpc", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, wantStatus, resp.StatusCode)
	_ = resp.Body.Close()
}
