package ratelimit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromEnvironmentMaxBuckets(t *testing.T) {
	t.Setenv("RATE_LIMIT_MAX_BUCKETS", "42")
	require.Equal(t, 42, FromEnvironment().MaxBuckets)
}

func TestFromEnvironmentMaxBucketsInvalidFallsBack(t *testing.T) {
	t.Setenv("RATE_LIMIT_MAX_BUCKETS", "nope")
	require.Equal(t, defaultMaxBuckets, FromEnvironment().MaxBuckets)
}
