package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
)

func TestHTTPMiddlewareDisabledPassesThrough(t *testing.T) {
	cfg := Config{Enabled: false}
	h := HTTPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/any")
	require.NoError(t, err)
	require.Equal(t, http.StatusTeapot, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddlewareSkipsMCPSSE(t *testing.T) {
	cfg := Config{Enabled: true, RPS: 1, Burst: 1}
	h := HTTPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res1, err := http.Get(ts.URL + "/mcp/rpc")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res1.StatusCode)
	require.NoError(t, res1.Body.Close())

	res2, err := http.Get(ts.URL + "/mcp/rpc")
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, res2.StatusCode)
	require.NoError(t, res2.Body.Close())

	resSSE, err := http.Get(ts.URL + mcpwire.PathMCPSSE)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resSSE.StatusCode)
	require.NoError(t, resSSE.Body.Close())
}

func TestHTTPMiddlewareSkipsHealthPaths(t *testing.T) {
	cfg := Config{Enabled: true, RPS: 0.001, Burst: 1}
	h := HTTPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res, err := http.Get(ts.URL + mcpwire.PathHealthz)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func TestHTTPMiddleware429WhenExhausted(t *testing.T) {
	cfg := Config{Enabled: true, RPS: 1, Burst: 1}
	var innerHits int
	h := HTTPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerHits++
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	res1, err := http.Get(ts.URL + "/mcp/rpc")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res1.StatusCode)
	require.NoError(t, res1.Body.Close())

	res2, err := http.Get(ts.URL + "/mcp/rpc")
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, res2.StatusCode)
	require.NoError(t, res2.Body.Close())

	require.Equal(t, 1, innerHits)
}

func TestLimiterKeyUsesSubjectWhenPresent(t *testing.T) {
	cfg := Config{Enabled: true, RPS: 1, Burst: 1}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := HTTPMiddleware(cfg)(inner)

	reqA := httptest.NewRequest(http.MethodGet, "/mcp/rpc", nil)
	reqA = reqA.WithContext(hostctx.WithSubjectID(context.Background(), "user-a"))
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, reqA)
	require.Equal(t, http.StatusOK, recA.Code)

	reqB := httptest.NewRequest(http.MethodGet, "/mcp/rpc", nil)
	reqB = reqB.WithContext(hostctx.WithSubjectID(context.Background(), "user-b"))
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	require.Equal(t, http.StatusOK, recB.Code)
}

func TestBucketMapEvictsLRUWhenAtCap(t *testing.T) {
	bm := newBucketMap(2, time.Hour)
	lim := rate.Limit(100)
	burst := 10
	now := time.Now()

	require.True(t, bm.allow("a", lim, burst, now))
	require.True(t, bm.allow("b", lim, burst, now))
	require.Equal(t, 2, bm.len())
	require.True(t, bm.contains("a"))
	require.True(t, bm.contains("b"))

	require.True(t, bm.allow("c", lim, burst, now))
	require.Equal(t, 2, bm.len())
	require.False(t, bm.contains("a"))
	require.True(t, bm.contains("b"))
	require.True(t, bm.contains("c"))
}

func TestHTTPMiddlewareCapsUniqueKeys(t *testing.T) {
	cfg := Config{Enabled: true, RPS: 100, Burst: 100, MaxBuckets: 3}
	h := HTTPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	subjects := []string{"u1", "u2", "u3", "u4", "u5"}
	for _, sub := range subjects {
		req := httptest.NewRequest(http.MethodGet, "/mcp/rpc", nil)
		req = req.WithContext(hostctx.WithSubjectID(context.Background(), sub))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "subject %s", sub)
	}

	req := httptest.NewRequest(http.MethodGet, "/mcp/rpc", nil)
	req = req.WithContext(hostctx.WithSubjectID(context.Background(), "u1"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHTTPMiddlewareEvictsStaleBuckets(t *testing.T) {
	cfg := Config{Enabled: true, RPS: 100, Burst: 100, BucketIdleTTL: 20 * time.Millisecond}
	var hits atomic.Int32
	h := HTTPMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp/rpc", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	time.Sleep(30 * time.Millisecond)

	req2 := httptest.NewRequest(http.MethodPost, "/mcp/rpc", nil)
	req2.RemoteAddr = "192.0.2.2:5678"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	req3 := httptest.NewRequest(http.MethodPost, "/mcp/rpc", nil)
	req3.RemoteAddr = "192.0.2.1:1234"
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)
	require.Equal(t, int32(3), hits.Load())
}
