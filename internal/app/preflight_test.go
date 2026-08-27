package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestReadinessProbesReuseTheConnection(t *testing.T) {
	var opened atomic.Int32
	body := strings.Repeat("healthy ", 64)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	client := srv.Client()
	for i := 0; i < 4; i++ {
		require.NoError(t, probeHealthPath(context.Background(), client, srv.URL, "/healthz"))
	}

	require.EqualValues(t, 1, opened.Load(),
		"a probe that closes the response body without reading it cannot reuse the connection, "+
			"so every readiness check pays a fresh handshake")
}
