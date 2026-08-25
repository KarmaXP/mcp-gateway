package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/syslog"
	"strings"
	"sync"
)

type syslogWriter interface {
	Info(string) error
	Close() error
}

type syslogAuditSink struct {
	mu     sync.Mutex
	writer syslogWriter
}

func NewSyslogAuditSink(network, address string) (*syslogAuditSink, error) {
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		network = "udp"
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("policy: syslog address is required")
	}
	w, err := syslog.Dial(network, address, syslog.LOG_INFO|syslog.LOG_AUTH, "mcp-gateway")
	if err != nil {
		return nil, fmt.Errorf("policy: create syslog writer: %w", err)
	}
	return &syslogAuditSink{writer: w}, nil
}

func (s *syslogAuditSink) Emit(_ context.Context, rec AuditRecord) error {
	payload, err := marshalAuditRecord(rec)
	if err != nil {
		return err
	}

	if s == nil || s.writer == nil {
		return fmt.Errorf("policy: syslog audit sink is not initialized")
	}

	s.mu.Lock()
	writeErr := s.writer.Info(payload)
	s.mu.Unlock()
	return writeErr
}

func (s *syslogAuditSink) Close() error {
	if s == nil || s.writer == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writer.Close()
}

func marshalAuditRecord(rec AuditRecord) (string, error) {
	msg := map[string]any{
		AuditMessageKey:    true,
		"outcome":          rec.Outcome,
		"reason":           rec.Reason,
		"policy_version":   rec.PolicyVersion,
		"subject_sha256_8": rec.SubjectSHA256,
	}
	if rec.ToolName != "" {
		msg["tool_name"] = rec.ToolName
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("policy: marshal syslog audit record: %w", err)
	}
	return string(raw), nil
}
