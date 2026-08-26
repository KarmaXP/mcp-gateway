package multiplex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type countingUpstream struct {
	inner       upstream.Client
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func (b *countingUpstream) ID() string     { return b.inner.ID() }
func (b *countingUpstream) Prefix() string { return b.inner.Prefix() }

func (b *countingUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if req.Method == "tools/call" {
		cur := b.inFlight.Add(1)
		defer b.inFlight.Add(-1)
		for {
			prev := b.maxInFlight.Load()
			if cur <= prev || b.maxInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
	}
	return b.inner.Call(ctx, req)
}

func TestToolsCallGlobalMaxInFlightBlocksConcurrentCalls(t *testing.T) {
	slow := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	slow.ToolsCallDelay = 220 * time.Millisecond
	up := &countingUpstream{inner: slow}
	m, err := New(
		context.Background(),
		[]upstream.Client{up},
		WithListTTL(0),
		WithCallTimeout(3*time.Second),
		WithGlobalMaxInFlight(2),
	)
	require.NoError(t, err)

	params, err := json.Marshal(map[string]any{
		"name":      "alpha__echo",
		"arguments": map[string]any{},
	})
	require.NoError(t, err)

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hostID := json.RawMessage(fmt.Sprintf("%d", i+1))
			resp, callErr := m.ToolsCall(context.Background(), hostID, params)
			if callErr != nil {
				errCh <- callErr
				return
			}
			if resp == nil || resp.Error != nil {
				errCh <- fmt.Errorf("unexpected tools/call response: %#v", resp)
				return
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)
	close(errCh)
	for callErr := range errCh {
		require.NoError(t, callErr)
	}
	require.EqualValues(t, 2, up.maxInFlight.Load())
	require.GreaterOrEqual(t, elapsed, 400*time.Millisecond, "third call should wait behind global max_in_flight=2")
}
