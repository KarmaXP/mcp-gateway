package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/stretchr/testify/require"
)

type fakeAuditSink struct {
	closed bool
}

func (f *fakeAuditSink) Emit(context.Context, policy.AuditRecord) error {
	return nil
}

func (f *fakeAuditSink) Close() error {
	f.closed = true
	return nil
}

func TestConfigureAuditSink_Slog(t *testing.T) {
	auditor, cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{AuditSink: config.PolicyAuditSinkSlog},
	}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, auditor)
	require.Nil(t, cleanup)
}

func TestConfigureAuditSink_SyslogRequiresAddress(t *testing.T) {
	_, _, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{AuditSink: config.PolicyAuditSinkSyslog},
	}, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy.audit_syslog_address")
}

func TestConfigureAuditSink_SyslogCleanupClosesSink(t *testing.T) {
	sink := &fakeAuditSink{}
	newSyslog := func(network, address string) (auditSinkCloser, error) { return sink, nil }

	auditor, cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{
			AuditSink:          config.PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
			AuditSyslogAddress: "127.0.0.1:514",
		},
	}, newSyslog, nil)
	require.NoError(t, err)
	require.NotNil(t, auditor)
	require.NotNil(t, cleanup)
	require.False(t, sink.closed)
	cleanup()
	require.True(t, sink.closed)
}

func TestPreflightStrictEnabled(t *testing.T) {
	t.Setenv("GATEWAY_PREFLIGHT_STRICT", "")
	require.False(t, preflightStrictEnabled(os.Getenv))

	t.Setenv("GATEWAY_PREFLIGHT_STRICT", "true")
	require.True(t, preflightStrictEnabled(os.Getenv))
}

func TestPreflightQdrantSkipsWhenRouterOff(t *testing.T) {
	err := preflightQdrant(context.Background(), config.GatewayConfig{
		SemanticRouter: config.SemanticRouterSettings{Mode: "off"},
	}, os.Getenv)
	require.NoError(t, err)
}

func TestConfigureAuditSink_SyslogInitError(t *testing.T) {
	newSyslog := func(network, address string) (auditSinkCloser, error) { return nil, errors.New("dial failed") }

	auditor, cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{
			AuditSink:          config.PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
			AuditSyslogAddress: "127.0.0.1:514",
		},
	}, newSyslog, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dial failed")
	require.Nil(t, auditor)
	require.Nil(t, cleanup)
}
