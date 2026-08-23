package app

import (
	"fmt"
	"log/slog"

	"github.com/KarmaXP/mcp-gateway/internal/config"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
)

type auditSinkCloser interface {
	policy.AuditSink
	Close() error
}

type syslogSinkFactory func(network, address string) (auditSinkCloser, error)

func defaultSyslogSinkFactory(network, address string) (auditSinkCloser, error) {
	return policy.NewSyslogAuditSink(network, address)
}

func configureAuditSink(cfg config.GatewayConfig, newSyslog syslogSinkFactory) (func(), error) {
	if newSyslog == nil {
		newSyslog = defaultSyslogSinkFactory
	}
	auditCfg, err := cfg.ResolvePolicyAuditSink()
	if err != nil {
		return nil, err
	}
	switch auditCfg.SinkType {
	case config.PolicyAuditSinkSlog:
		policy.SetAuditSink(policy.WithDecisionMetrics(policy.SlogAuditSink{}, policyDecisionMetrics{}))
		slog.Info("policy audit sink configured", "sink", config.PolicyAuditSinkSlog)
		return nil, nil
	case config.PolicyAuditSinkSyslog:
		sink, err := newSyslog(auditCfg.SyslogNetwork, auditCfg.SyslogAddress)
		if err != nil {
			return nil, err
		}
		policy.SetAuditSink(policy.WithDecisionMetrics(sink, policyDecisionMetrics{}))
		slog.Info("policy audit sink configured",
			"sink", config.PolicyAuditSinkSyslog,
			"network", auditCfg.SyslogNetwork,
			"address", auditCfg.SyslogAddress,
		)
		return func() {
			if err := sink.Close(); err != nil {
				slog.Warn("policy audit sink close", "err", err)
			}
		}, nil
	default:
		return nil, fmt.Errorf("unsupported audit sink type %q", auditCfg.SinkType)
	}
}
