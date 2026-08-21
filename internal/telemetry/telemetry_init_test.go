package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInitShutdownWithoutOTLPEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "mcp-gateway-test")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}

func TestInitWithOTLPEndpointDoesNotFailWhenCollectorUnreachable(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:9")
	shutdown, err := Init(context.Background(), "mcp-gateway-otlp-test")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	tctx, span := StartSpan(context.Background(), "smoke.export.path")
	span.End()
	_ = tctx

	ctx2, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := shutdown(ctx2); err != nil {
		require.Contains(t, err.Error(), "127.0.0.1:9")
	}
}

func TestInitImplicitHTTPSchemeForBareHost(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:9")
	shutdown, err := Init(context.Background(), "mcp-gateway-bare-endpoint")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:9", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		require.Contains(t, err.Error(), "127.0.0.1:9")
	}
}

func recordingCollector(t *testing.T, prefix string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux := http.NewServeMux()
	if prefix == "" {
		mux.Handle("/", h)
	} else {
		mux.Handle(prefix+"/", h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

func exportOneSpan(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	shutdown, err := Init(context.Background(), "mcp-gateway-signal-path-test")
	require.NoError(t, err)

	_, span := StartSpan(context.Background(), "signal.path.span")
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}

func TestInitExportsToOTLPSignalPaths(t *testing.T) {
	srv, paths := recordingCollector(t, "")
	exportOneSpan(t, srv.URL)
	require.Contains(t, paths(), otlpTracesPath)
	require.Contains(t, paths(), otlpMetricsPath)
}

func TestInitExportsUnderAnEndpointBasePath(t *testing.T) {
	srv, paths := recordingCollector(t, "/otlp")
	exportOneSpan(t, srv.URL+"/otlp")
	require.Contains(t, paths(), "/otlp"+otlpTracesPath)
	require.Contains(t, paths(), "/otlp"+otlpMetricsPath)
}

func TestOTLPSignalURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "bare host gets the http scheme", endpoint: "127.0.0.1:4318", want: "http://127.0.0.1:4318/v1/traces"},
		{name: "explicit http is preserved", endpoint: "http://collector:4318", want: "http://collector:4318/v1/traces"},
		{name: "https is preserved", endpoint: "https://collector:4318", want: "https://collector:4318/v1/traces"},
		{name: "trailing slash is not duplicated", endpoint: "http://collector:4318/", want: "http://collector:4318/v1/traces"},
		{name: "base path is preserved", endpoint: "http://collector:4318/otlp", want: "http://collector:4318/otlp/v1/traces"},
		{name: "base path with trailing slash", endpoint: "http://collector:4318/otlp/", want: "http://collector:4318/otlp/v1/traces"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, otlpSignalURL(tc.endpoint, otlpTracesPath))
		})
	}
}
