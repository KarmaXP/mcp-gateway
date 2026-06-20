package mcpstdio

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
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

func TestDispatchResponseDeliversToPending(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	ch := make(chan *rpc.Response, 1)
	c.pendMu.Lock()
	c.pending["1"] = ch
	c.pendMu.Unlock()

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      1,
		"result":  map[string]any{"ok": true},
	})
	require.NoError(t, err)
	c.dispatch(raw)

	select {
	case resp := <-ch:
		require.NotNil(t, resp)
		require.Nil(t, resp.Error)
	default:
		t.Fatal("expected response on pending channel")
	}
}

func TestDispatchResponseAbortsPendingWhenChannelFull(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
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

func TestCallClearsPendingErrOnContextCancel(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"sleep", "3600"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, c.ensure(context.Background()))

	const key = "3"
	ctx, cancel := context.WithCancel(context.Background())

	var callErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, callErr = c.Call(ctx, &rpc.Request{
			JSONRPC: rpc.JSONRPCVersion,
			Method:  "tools/list",
			ID:      json.RawMessage(`3`),
		})
	}()

	require.Eventually(t, func() bool {
		c.pendMu.Lock()
		_, ok := c.pending[key]
		c.pendMu.Unlock()
		return ok
	}, time.Second, 5*time.Millisecond)

	c.pendMu.Lock()
	c.pendingErr[key] = errors.New("stale pending delivery error")
	c.pendMu.Unlock()
	cancel()
	wg.Wait()

	require.ErrorIs(t, callErr, context.Canceled)

	c.pendMu.Lock()
	_, leaked := c.pendingErr[key]
	c.pendMu.Unlock()
	require.False(t, leaked, "pendingErr must be cleared when Call exits on context cancel")
}

func TestCallRejectsDuplicateJSONRPCID(t *testing.T) {
	t.Parallel()
	c, cleanup, err := NewStdioMCPUpstream(context.Background(), "u1", "alpha", []string{"true"}, nil, 1)
	require.NoError(t, err)
	defer cleanup()

	ch := make(chan *rpc.Response, pendingJSONRPCChannelCap)
	c.pendMu.Lock()
	c.pending["1"] = ch
	c.pendMu.Unlock()

	_, err = c.Call(context.Background(), &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`1`),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate jsonrpc id")
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

func TestIDKey(t *testing.T) {
	t.Parallel()
	require.Empty(t, idKey(nil))
	require.Equal(t, "42", idKey(json.RawMessage(`42`)))
}

func TestCloseReapsChildProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("child reaping check is unix-specific")
	}

	ctx := context.Background()
	c, cleanup, err := NewStdioMCPUpstream(ctx, "u1", "alpha", []string{"sleep", "3600"}, nil, 1)
	require.NoError(t, err)
	require.NoError(t, c.ensure(ctx))

	pid := c.cmd.Process.Pid
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
	require.NoError(t, c.cmd.Process.Kill())

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
	require.NoError(t, c.cmd.Process.Kill())

	require.Eventually(t, func() bool {
		return c.deadError() != nil
	}, 2*time.Second, 10*time.Millisecond)

	err = c.ensure(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "process exited")
}
