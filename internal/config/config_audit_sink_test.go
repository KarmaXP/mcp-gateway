package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePolicyAuditSink_DefaultsToSlog(t *testing.T) {
	var cfg GatewayConfig
	got, err := cfg.ResolvePolicyAuditSink()
	require.NoError(t, err)
	require.Equal(t, PolicyAuditSinkSlog, got.SinkType)
	require.Equal(t, "udp", got.SyslogNetwork)
	require.Empty(t, got.SyslogAddress)
}

func TestResolvePolicyAuditSink_SyslogDefaultsNetwork(t *testing.T) {
	cfg := GatewayConfig{
		Policy: PolicySettings{
			AuditSink:          PolicyAuditSinkSyslog,
			AuditSyslogAddress: "127.0.0.1:514",
		},
	}
	got, err := cfg.ResolvePolicyAuditSink()
	require.NoError(t, err)
	require.Equal(t, PolicyAuditSinkSyslog, got.SinkType)
	require.Equal(t, "udp", got.SyslogNetwork)
	require.Equal(t, "127.0.0.1:514", got.SyslogAddress)
}

func TestResolvePolicyAuditSink_SyslogRequiresAddress(t *testing.T) {
	cfg := GatewayConfig{
		Policy: PolicySettings{
			AuditSink:          PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
		},
	}
	_, err := cfg.ResolvePolicyAuditSink()
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy.audit_syslog_address")
}

func TestResolvePolicyAuditSink_RejectsUnknownType(t *testing.T) {
	cfg := GatewayConfig{
		Policy: PolicySettings{
			AuditSink: "not-real",
		},
	}
	_, err := cfg.ResolvePolicyAuditSink()
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy.audit_sink")
}

func TestLoadPolicyAuditSinkEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	err := os.WriteFile(p, []byte(`
backends:
  - id: one
    prefix: a
    url: http://example.invalid:9
policy:
  audit_sink: slog
`), 0o644)
	require.NoError(t, err)
	t.Setenv("MCP_GATEWAY_CONFIG", p)
	t.Setenv("MCP_GATEWAY_BACKENDS", "")
	t.Setenv("POLICY_AUDIT_SINK", "syslog")
	t.Setenv("POLICY_AUDIT_SYSLOG_NETWORK", "tcp")
	t.Setenv("POLICY_AUDIT_SYSLOG_ADDRESS", "127.0.0.1:1514")

	cfg, err := Load()
	require.NoError(t, err)
	got, err := cfg.ResolvePolicyAuditSink()
	require.NoError(t, err)
	require.Equal(t, PolicyAuditSinkSyslog, got.SinkType)
	require.Equal(t, "tcp", got.SyslogNetwork)
	require.Equal(t, "127.0.0.1:1514", got.SyslogAddress)
}
