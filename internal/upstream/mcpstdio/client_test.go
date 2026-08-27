package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestNewStdioMCPUpstream_EmptyCommand(t *testing.T) {
	t.Parallel()
	_, _, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", nil, nil, 1)
	require.Error(t, err)
}

func TestDispatchNotificationInvokesCallback(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
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
		"method":  mcpwire.NotificationToolsListChanged,
	})
	require.NoError(t, err)
	c.dispatch(raw)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, seen)
	require.Equal(t, mcpwire.NotificationToolsListChanged, seen.Method)
}

func TestDispatchResponseIgnoresUnknownID(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      99,
		"result":  map[string]any{},
	})
	require.NoError(t, err)
	c.dispatch(raw)
}

func TestCloseReapsChildProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("child reaping check is unix-specific")
	}

	ctx := context.Background()
	c, cleanup, err := NewStdioMCPUpstream(ctx, "u1", "alpha", []string{"sleep", "3600"}, nil, 1)
	require.NoError(t, err)
	require.NoError(t, c.ensure(ctx))

	pid := c.proc.Load().cmd.Process.Pid
	cleanup()

	var status syscall.WaitStatus
	_, waitErr := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	require.Error(t, waitErr)
	require.ErrorIs(t, waitErr, syscall.ECHILD)
}

func TestCallReturnsAfterProcessExit(t *testing.T) {
	ctx := context.Background()
	c, cleanup, err := NewStdioMCPUpstream(ctx, "u1", "alpha", []string{"sleep", "3600"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, c.ensure(ctx))
	require.NoError(t, c.proc.Load().cmd.Process.Kill())

	require.Eventually(t, func() bool {
		return c.deadError() != nil
	}, 2*time.Second, 10*time.Millisecond)

	callCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = c.Call(callCtx, &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`1`),
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "process exited")
}

func TestEnsureFailsAfterProcessExit(t *testing.T) {
	ctx := context.Background()
	c, cleanup, err := NewStdioMCPUpstream(ctx, "u1", "alpha", []string{"sleep", "3600"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, c.ensure(ctx))
	require.NoError(t, c.proc.Load().cmd.Process.Kill())

	require.Eventually(t, func() bool {
		return c.deadError() != nil
	}, 2*time.Second, 10*time.Millisecond)

	err = c.ensure(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "process exited")
}

func TestChildDoesNotInheritTheGatewayEnvironment(t *testing.T) {
	t.Setenv("MCP_GATEWAY_BACKENDS", "id=other,auth_token=super-secret")
	t.Setenv("JWT_PUBLIC_KEY_PEM", "BEGIN-PUBLIC-KEY-material")

	script := `read line; printf '{"jsonrpc":"2.0","id":1,"result":{"backends":"%s","jwt":"%s","own":"%s","path":"%s"}}\n' ` +
		`"$MCP_GATEWAY_BACKENDS" "$JWT_PUBLIC_KEY_PEM" "$UPSTREAM_OWN" "${PATH:+set}"`

	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha",
		[]string{"sh", "-c", script}, []string{"UPSTREAM_OWN=from-config"}, 1)
	require.NoError(t, err)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Call(ctx, &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion, Method: "tools/list", ID: json.RawMessage(`1`),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	var seen struct {
		Backends string `json:"backends"`
		JWT      string `json:"jwt"`
		Own      string `json:"own"`
		Path     string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &seen))
	require.Empty(t, seen.Backends, "MCP_GATEWAY_BACKENDS carries every upstream auth_token")
	require.Empty(t, seen.JWT, "the child must not see the gateway signing key")
	require.Equal(t, "from-config", seen.Own, "the upstream's own env must still reach it")
	require.Equal(t, "set", seen.Path, "PATH must survive or nothing can be executed")
}

func TestCloseDuringStartLeavesNoRaceAndNoOrphan(t *testing.T) {
	for i := 0; i < 40; i++ {
		c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"sleep", "3600"}, nil, 1)
		require.NoError(t, err)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = c.ensure(context.Background()) }()
		go func() { defer wg.Done(); cleanup() }()
		wg.Wait()
	}
}

func TestCallDeadlineIsNotBlockedByAStuckWrite(t *testing.T) {
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"sleep", "3600"}, nil, 4)
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, c.ensure(context.Background()))

	// sleep never drains stdin, so a payload past the pipe buffer wedges the writer.
	args, err := json.Marshal(map[string]any{"blob": strings.Repeat("x", 1<<20)})
	require.NoError(t, err)
	go func() {
		_, _ = c.Call(context.Background(), &rpc.Request{
			JSONRPC: rpc.JSONRPCVersion, Method: "tools/call",
			ID: json.RawMessage(`1`), Params: args,
		})
	}()
	time.Sleep(300 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, &rpc.Request{
			JSONRPC: rpc.JSONRPCVersion, Method: "tools/list", ID: json.RawMessage(`2`),
		})
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("a caller with its own 100ms deadline must not queue behind a wedged write")
	}
}

type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestOversizedFrameKillsTheUpstreamInsteadOfBuffering(t *testing.T) {
	// 9 MiB with no newline, past defaults.MaxUpstreamFrameBytes.
	script := `head -c 9000000 /dev/zero | tr "\0" "x"; sleep 3600`
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"sh", "-c", script}, nil, 1)
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, c.ensure(context.Background()))

	require.Eventually(t, func() bool {
		return c.deadError() != nil
	}, 15*time.Second, 50*time.Millisecond, "an upstream sending an unterminated frame must be dropped")
	require.ErrorContains(t, c.deadError(), "frame exceeds the maximum size")
}

func TestChildStderrIsLoggedWithItsUpstreamID(t *testing.T) {
	var out syncBuf
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u-stderr", "alpha",
		[]string{"sh", "-c", `echo "child said boom" >&2; sleep 3600`}, nil, 1)
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, c.ensure(context.Background()))

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "child said boom")
	}, 10*time.Second, 25*time.Millisecond, "child stderr must reach slog, not the gateway's own stderr")
	require.Contains(t, out.String(), `"upstream_id":"u-stderr"`)
}
