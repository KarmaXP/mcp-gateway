package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestCreateRefusesBeyondTheConcurrentSessionCap(t *testing.T) {
	mpx, err := multiplex.New(context.Background(), []upstream.Client{mock.NewMockUpstream("b1", "alpha", []string{"echo"})}, multiplex.WithListTTL(0))
	require.NoError(t, err)
	sm := NewSessionManager(context.Background(), mpx)

	ctx := context.Background()
	for range defaults.MaxConcurrentSSESessions {
		_, err := sm.Create(ctx)
		require.NoError(t, err)
	}

	_, err = sm.Create(ctx)
	require.ErrorIs(t, err, ErrTooManySessions, "an unauthenticated GET can create sessions without bound")

	sm.Remove(anySessionID(t, sm))
	_, err = sm.Create(ctx)
	require.NoError(t, err, "a closed session must give its slot back")
}

func anySessionID(t *testing.T, sm *SessionManager) string {
	t.Helper()
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for id := range sm.sessions {
		return id
	}
	t.Fatal("no live session")
	return ""
}
