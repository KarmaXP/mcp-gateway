package main

import (
	"context"
	"errors"
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
	})
	require.NoError(t, err)
	require.Nil(t, cleanup)
}

func TestConfigureAuditSink_SyslogRequiresAddress(t *testing.T) {
	_, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{AuditSink: config.PolicyAuditSinkSyslog},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "policy.audit_syslog_address")
}

func TestConfigureAuditSink_SyslogCleanupClosesSink(t *testing.T) {
	t.Cleanup(func() {
		newSyslogAuditSink = func(network, address string) (auditSinkCloser, error) {
			return policy.NewSyslogAuditSink(network, address)
		}
		policy.SetAuditSink(nil)
	})

	sink := &fakeAuditSink{}
	newSyslogAuditSink = func(network, address string) (auditSinkCloser, error) {
		return sink, nil
	}

	cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{
			AuditSink:          config.PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
			AuditSyslogAddress: "127.0.0.1:514",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	require.False(t, sink.closed)
	cleanup()
	require.True(t, sink.closed)
}

func TestPreflightStrictEnabled(t *testing.T) {
	t.Setenv("GATEWAY_PREFLIGHT_STRICT", "")
	require.False(t, preflightStrictEnabled())

	t.Setenv("GATEWAY_PREFLIGHT_STRICT", "true")
	require.True(t, preflightStrictEnabled())
}

func TestPreflightQdrantSkipsWhenRouterOff(t *testing.T) {
	err := preflightQdrant(config.GatewayConfig{
		SemanticRouter: config.SemanticRouterSettings{Mode: "off"},
	})
	require.NoError(t, err)
}

func TestConfigureAuditSink_SyslogInitError(t *testing.T) {
	t.Cleanup(func() {
		newSyslogAuditSink = func(network, address string) (auditSinkCloser, error) {
			return policy.NewSyslogAuditSink(network, address)
		}
		policy.SetAuditSink(nil)
	})

	newSyslogAuditSink = func(network, address string) (auditSinkCloser, error) {
		return nil, errors.New("dial failed")
	}

	cleanup, err := configureAuditSink(config.GatewayConfig{
		Policy: config.PolicySettings{
			AuditSink:          config.PolicyAuditSinkSyslog,
			AuditSyslogNetwork: "udp",
			AuditSyslogAddress: "127.0.0.1:514",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dial failed")
	require.Nil(t, cleanup)
}
