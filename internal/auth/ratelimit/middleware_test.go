package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

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
