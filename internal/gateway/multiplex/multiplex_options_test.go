package multiplex

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func TestMultiplexerPrefixMapAndTimeoutOptions(t *testing.T) {
	b1 := mock.NewMockUpstream("id1", "alpha", []string{"echo"})
	b2 := mock.NewMockUpstream("id2", "beta", []string{"ping"})
	a, err := New(context.Background(), []upstream.Client{b1, b2},
		WithInitTimeout(time.Millisecond),
		WithListTimeout(2*time.Millisecond),
		WithCallTimeout(3*time.Millisecond),
		WithListTTL(0),
	)
	require.NoError(t, err)
	m := a.prefixToUpstreamID()
	require.Equal(t, "id1", m["alpha"])
	require.Equal(t, "id2", m["beta"])
}
