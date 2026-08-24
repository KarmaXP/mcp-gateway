package app

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

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

func configureAuditSink(cfg config.GatewayConfig, newSyslog syslogSinkFactory, getenv func(string) string) (*policy.Auditor, func(), error) {
	if newSyslog == nil {
		newSyslog = defaultSyslogSinkFactory
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	pepper := []byte(strings.TrimSpace(getenv("POLICY_AUDIT_SUBJECT_PEPPER")))
	auditCfg, err := cfg.ResolvePolicyAuditSink()
	if err != nil {
		return nil, nil, err
	}
	switch auditCfg.SinkType {
	case config.PolicyAuditSinkSlog:
		slog.Info("policy audit sink configured", "sink", config.PolicyAuditSinkSlog, "subject_pepper", pepperState(pepper))
		return policy.NewAuditor(policy.WithDecisionMetrics(policy.SlogAuditSink{}, policyDecisionMetrics{}), pepper), nil, nil
	case config.PolicyAuditSinkSyslog:
		sink, err := newSyslog(auditCfg.SyslogNetwork, auditCfg.SyslogAddress)
		if err != nil {
			return nil, nil, err
		}
		slog.Info("policy audit sink configured",
			"sink", config.PolicyAuditSinkSyslog,
			"network", auditCfg.SyslogNetwork,
			"address", auditCfg.SyslogAddress,
			"subject_pepper", pepperState(pepper),
		)
		return policy.NewAuditor(policy.WithDecisionMetrics(sink, policyDecisionMetrics{}), pepper), func() {
			if err := sink.Close(); err != nil {
				slog.Warn("policy audit sink close", "err", err)
			}
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported audit sink type %q", auditCfg.SinkType)
	}
}

func pepperState(pepper []byte) string {
	if len(pepper) == 0 {
		return "absent"
	}
	return "set"
}
