package session

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestBroadcastWorkersStopWithTheLifecycleContext(t *testing.T) {
	const managers = 4
	expected := managers * defaults.SessionBroadcastMaxConcurrency

	mpx, err := multiplex.New(context.Background(), []upstream.Client{mock.NewMockUpstream("b1", "alpha", []string{"echo"})}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	baseline := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	for range managers {
		NewSessionManager(ctx, mpx)
	}
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine()-baseline >= expected
	}, 2*time.Second, 10*time.Millisecond, "each manager should start its broadcast workers")

	cancel()
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine()-baseline < expected/2
	}, 5*time.Second, 20*time.Millisecond,
		"broadcast workers must stop when the lifecycle context is done, or every manager leaks %d goroutines",
		defaults.SessionBroadcastMaxConcurrency)
}
