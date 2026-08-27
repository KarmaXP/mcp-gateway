package httpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

func newOriginTestServer(t *testing.T, origins []string) *httptest.Server {
	t.Helper()

	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New(context.Background(), []upstream.Client{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	srv := New(context.Background(), agg, "", WithOriginAllowList(origins))
	return httptest.NewServer(srv)
}

func TestOriginAllowListDisabledWhenEmpty(t *testing.T) {
	ts := newOriginTestServer(t, nil)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+PathMCPSSE, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://blocked.example")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NotEmpty(t, res.Header.Get(HeaderMCPSessionID))
}

func TestOriginCheckIsSkippedOnRPCWhenTheAllowListIsEmpty(t *testing.T) {
	ts := newOriginTestServer(t, nil)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Origin", "https://blocked.example")
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.NotEqual(t, http.StatusForbidden, res.StatusCode,
		"an empty allow-list disables the Origin check, as .env.example and docs/configuration.md both state")
	require.NoError(t, res.Body.Close())
}

func TestOriginAllowListAllowsMissingOrigin(t *testing.T) {
	ts := newOriginTestServer(t, []string{"https://allowed.example"})
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + PathMCPSSE)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NotEmpty(t, res.Header.Get(HeaderMCPSessionID))
}

func TestOriginAllowListRejectsSSEOriginMismatch(t *testing.T) {
	ts := newOriginTestServer(t, []string{"https://allowed.example"})
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+PathMCPSSE, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", " https://blocked.example ")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	require.Contains(t, res.Header.Get("Content-Type"), "text/plain")
	body, err := ioReadAllString(res.Body)
	require.NoError(t, err)
	require.Contains(t, body, "origin not allowed")
}

func TestOriginAllowListAllowsSSEOriginMatchAfterTrim(t *testing.T) {
	ts := newOriginTestServer(t, []string{" https://allowed.example "})
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+PathMCPSSE, nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://allowed.example")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NotEmpty(t, res.Header.Get(HeaderMCPSessionID))
	require.NoError(t, res.Body.Close())
}

func TestOriginAllowListRejectsRPCOriginMismatch(t *testing.T) {
	ts := newOriginTestServer(t, []string{"https://allowed.example"})
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Origin", "https://blocked.example")
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, res.StatusCode)
	require.Contains(t, res.Header.Get("Content-Type"), "text/plain")
	body, err := ioReadAllString(res.Body)
	require.NoError(t, err)
	require.Contains(t, body, "origin not allowed")
}

func TestOriginAllowListAllowsRPCOriginMatch(t *testing.T) {
	ts := newOriginTestServer(t, []string{"https://allowed.example"})
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+PathMCPRPC, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Origin", "https://allowed.example")
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	require.NoError(t, res.Body.Close())
}

func ioReadAllString(body io.ReadCloser) (string, error) {
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
