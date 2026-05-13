package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportPartialFailuresYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: a
    prefix: alpha
    url: http://127.0.0.1:1
aggregation:
  report_partial_failures: true
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.Aggregation.ReportPartialFailures)
}

func TestReportPartialFailuresEnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: a
    prefix: alpha
    url: http://127.0.0.1:1
aggregation:
  report_partial_failures: false
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("AGGREGATION_REPORT_PARTIAL_FAILURES", "true")

	cfg, err := Load()
	require.NoError(t, err)
	require.True(t, cfg.Aggregation.ReportPartialFailures)
}
