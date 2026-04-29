package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/multiplex"
)

func TestHTTPServerOptionsWithServer(t *testing.T) {
	cfg := auth.JWTAuthConfig{Mode: "none"}
	v, err := auth.NewValidator(cfg)
	require.NoError(t, err)

	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	agg, err := multiplex.New([]backend.Upstream{b1}, multiplex.WithListTTL(0))
	require.NoError(t, err)

	opts := HTTPServerOptions("test-svc", cfg, v)
	srv := httpserver.New(agg, "", opts...)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	res, err := http.Get(ts.URL + httpserver.PathHealthz)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, res.Body.Close())
}
