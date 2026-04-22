package aggregate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
)

func TestAggregatorPrefixMapAndTimeoutOptions(t *testing.T) {
	b1 := mock.New("id1", "alpha", []string{"echo"})
	b2 := mock.New("id2", "beta", []string{"ping"})
	a, err := New([]backend.Backend{b1, b2},
		WithInitTimeout(time.Millisecond),
		WithListTimeout(2*time.Millisecond),
		WithCallTimeout(3*time.Millisecond),
		WithListTTL(0),
	)
	require.NoError(t, err)
	m := a.PrefixToBackendID()
	require.Equal(t, "id1", m["alpha"])
	require.Equal(t, "id2", m["beta"])
}
