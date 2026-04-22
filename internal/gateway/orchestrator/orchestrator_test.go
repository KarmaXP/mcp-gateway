package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/auth"
	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/backend/mock"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/aggregate"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/httpserver"
)

func TestHTTPMiddlewareOptionsWithServer(t *testing.T) {
	cfg := auth.Config{Mode: "none"}
	v, err := auth.NewValidator(cfg)
	require.NoError(t, err)

	b1 := mock.New("b1", "alpha", []string{"echo"})
	agg, err := aggregate.New([]backend.Backend{b1}, aggregate.WithListTTL(0))
	require.NoError(t, err)

	opts := HTTPMiddlewareOptions("test-svc", cfg, v)
	srv := httpserver.New(agg, "", opts...)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NoError(t, res.Body.Close())
}
