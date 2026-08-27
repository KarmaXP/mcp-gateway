package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadinessProbesRunTogether(t *testing.T) {
	var inFlight, peak atomic.Int32
	slowOK := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			current := inFlight.Add(1)
			for {
				seen := peak.Load()
				if current <= seen || peak.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(80 * time.Millisecond)
			inFlight.Add(-1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
	}
	qdrant, embed := slowOK(), slowOK()
	t.Cleanup(qdrant.Close)
	t.Cleanup(embed.Close)

	checker := &dependencyReadinessChecker{httpClient: qdrant.Client(), qdrantURL: qdrant.URL, embedURL: embed.URL}
	require.NoError(t, checker.CheckReadiness(context.Background()))

	require.EqualValues(t, 2, peak.Load(),
		"a serial readiness check adds every dependency's latency to one probe deadline")
}

func TestReadinessReportsWhichDependencyIsUnhealthy(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(healthy.Close)

	checker := &dependencyReadinessChecker{httpClient: down.Client(), qdrantURL: healthy.URL, embedURL: down.URL}
	require.ErrorContains(t, checker.CheckReadiness(context.Background()), "embed dependency unhealthy")
}
