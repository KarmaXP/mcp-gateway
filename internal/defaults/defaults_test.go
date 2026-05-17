package defaults

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoreLimitsPositive(t *testing.T) {
	require.Positive(t, MaxMCPRPCBodyBytes)
	require.Positive(t, MaxToolArgumentsJSONBytes)
	require.Positive(t, MaxToolArgumentsJSONDepth)
	require.Positive(t, MaxToolArgumentsJSONKeys)
	require.Positive(t, UpstreamMaxConcurrency)
}

func TestHTTPTimeoutsOrdered(t *testing.T) {
	require.GreaterOrEqual(t, HTTPIdleTimeout, HTTPReadTimeout)
	require.GreaterOrEqual(t, MultiplexCallTimeout, MultiplexListTimeout)
	require.GreaterOrEqual(t, RouterEmbedTimeout, RouterQueryTimeout)
}
