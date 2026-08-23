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
	t.Cleanup(func() { policy.SetAuditSink(nil) })

	cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{AuditSink: config.PolicyAuditSinkSlog},
	}, nil)
	require.NoError(t, err)
	require.Nil(t, cleanup)
}

func TestConfigureAuditSink_SyslogRequiresAddress(t *testing.T) {
	_, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{AuditSink: config.PolicyAuditSinkSyslog},
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy.audit_syslog_address")
}

func TestConfigureAuditSink_SyslogCleanupClosesSink(t *testing.T) {
	t.Cleanup(func() { policy.SetAuditSink(nil) })

	sink := &fakeAuditSink{}
	newSyslog := func(network, address string) (auditSinkCloser, error) { return sink, nil }

	cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{
			AuditSink:          config.PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
			AuditSyslogAddress: "127.0.0.1:514",
		},
	}, newSyslog)
	require.NoError(t, err)
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
	t.Cleanup(func() { policy.SetAuditSink(nil) })

	newSyslog := func(network, address string) (auditSinkCloser, error) { return nil, errors.New("dial failed") }

	cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{
			AuditSink:          config.PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
			AuditSyslogAddress: "127.0.0.1:514",
		},
	}, newSyslog)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dial failed")
	require.Nil(t, cleanup)
}
