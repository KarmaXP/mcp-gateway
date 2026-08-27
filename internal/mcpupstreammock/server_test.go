package mcpupstreammock

import (
	"bufio"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
)

func TestToolsListAndCallEcho(t *testing.T) {
	s := startTestServer(t, Config{ServerName: "test-mock", Tools: []Tool{{
		Name:        "echo",
		Description: "echo",
		CallText:    "alpha-ok",
	}}})
	sessionID, sseBody := openSession(t, s)

	postRPC(t, s, sessionID, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`, http.StatusAccepted)
	requireEventually(t, sseBody, `"id":1`, `"result"`)

	postRPC(t, s, sessionID, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, http.StatusAccepted)
	requireEventually(t, sseBody, `"id":2`, "echo")

	postRPC(t, s, sessionID, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{}}}`, http.StatusAccepted)
	requireEventually(t, sseBody, `"id":3`, "alpha-ok")
}

func TestASecondSessionDoesNotInvalidateTheFirst(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	first, _ := openSession(t, s)
	_, _ = openSession(t, s)

	postRPC(t, s, first, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, http.StatusAccepted)
}

func TestUnknownMethodAnswersOnTheAskingSession(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	sessionID, sseBody := openSession(t, s)

	postRPC(t, s, sessionID, `{"jsonrpc":"2.0","id":9,"method":"ping/unknown"}`, http.StatusAccepted)
	requireEventually(t, sseBody, `"id":9`, "not found")
}

func TestNotificationGetsNoResponse(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	sessionID, sseBody := openSession(t, s)

	postRPC(t, s, sessionID, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`, http.StatusAccepted)
	requireEventually(t, sseBody, `"id":1`)

	postRPC(t, s, sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, http.StatusAccepted)
	require.Never(t, func() bool {
		return strings.Contains(sseBody(), `"code":`) || strings.Contains(sseBody(), "not found")
	}, 200*time.Millisecond, 20*time.Millisecond)
}

func TestUnknownSessionIsRefused(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	postRPC(t, s, "wrong-session", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, http.StatusUnauthorized)
}

func TestMissingSessionHeaderIsRefused(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	postRPC(t, s, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, http.StatusUnauthorized)
}

func TestInvalidRPCBodyIsRefused(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	sessionID, _ := openSession(t, s)
	postRPC(t, s, sessionID, `{not json`, http.StatusBadRequest)
}

func TestStartRequiresAListenAddress(t *testing.T) {
	_, err := Start(Config{})
	require.ErrorContains(t, err, "listen address required")
}

func TestAddrReportsTheBoundPort(t *testing.T) {
	s := startTestServer(t, Config{Tools: []Tool{{Name: "echo"}}})
	require.NotContains(t, s.Addr(), ":0", "a caller must be able to learn the port the mock actually took")
}

func startTestServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.ListenAddr = "127.0.0.1:0"
	s, err := Start(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func openSession(t *testing.T, s *Server) (string, func() string) {
	t.Helper()
	resp, err := http.Get("http://" + s.Addr() + mcpwire.PathMCPSSE)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	sessionID := resp.Header.Get(mcpwire.HeaderMCPSessionID)
	require.NotEmpty(t, sessionID)

	var mu sync.Mutex
	var buf strings.Builder
	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, readErr := reader.ReadString('\n')
			mu.Lock()
			buf.WriteString(line)
			mu.Unlock()
			if readErr != nil {
				return
			}
		}
	}()
	return sessionID, func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

func postRPC(t *testing.T, s *Server, sessionID, body string, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+s.Addr()+mcpwire.PathMCPRPC, strings.NewReader(body))
	require.NoError(t, err)
	if sessionID != "" {
		req.Header.Set(mcpwire.HeaderMCPSessionID, sessionID)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, wantStatus, resp.StatusCode)
	_ = resp.Body.Close()
}

func requireEventually(t *testing.T, sseBody func() string, needles ...string) {
	t.Helper()
	require.Eventually(t, func() bool {
		body := sseBody()
		for _, n := range needles {
			if !strings.Contains(body, n) {
				return false
			}
		}
		return true
	}, 2*time.Second, 20*time.Millisecond, "expected %v in the SSE stream", needles)
}
