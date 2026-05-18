package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestBucketMapEvictsIdleInBackground(t *testing.T) {
	bm := newBucketMap(100, 20*time.Millisecond)
	lim := rate.Limit(100)
	burst := 10

	require.True(t, bm.allow("idle-key", lim, burst, time.Now()))
	require.True(t, bm.contains("idle-key"))

	time.Sleep(30 * time.Millisecond)
	bm.sweepStale(time.Now())

	require.False(t, bm.contains("idle-key"))
	require.Equal(t, 0, bm.len())
}

func TestEvictionIntervalRespectsIdleTTL(t *testing.T) {
	require.Equal(t, time.Second, evictionInterval(500*time.Millisecond))
	require.Equal(t, 15*time.Second, evictionInterval(30*time.Second))
	require.Equal(t, 5*time.Minute, evictionInterval(30*time.Minute))
}
