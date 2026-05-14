package policy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSyslogWriter struct {
	msg      string
	writeErr error
	closed   bool
}

func (f *fakeSyslogWriter) Info(msg string) error {
	f.msg = msg
	return f.writeErr
}

func (f *fakeSyslogWriter) Close() error {
	f.closed = true
	return nil
}

func TestNewSyslogAuditSink_RequiresAddress(t *testing.T) {
	_, err := NewSyslogAuditSink("udp", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "syslog address is required")
}

func TestNewSyslogAuditSink_InvalidAddress(t *testing.T) {
	_, err := NewSyslogAuditSink("tcp", "127.0.0.1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "create syslog writer")
}

func TestSyslogAuditSink_EmitWritesExpectedFields(t *testing.T) {
	fw := &fakeSyslogWriter{}
	sink := &SyslogAuditSink{writer: fw}

	rec := AuditRecord{
		Outcome:       "allow",
		Reason:        "in_allow_list",
		ToolName:      "k8s__get_logs",
		SubjectSHA256: "abcd1234",
		PolicyVersion: "v1",
	}
	err := sink.Emit(context.Background(), rec)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(fw.msg), &payload))
	require.Equal(t, true, payload[AuditMessageKey])
	require.Equal(t, "allow", payload["outcome"])
	require.Equal(t, "in_allow_list", payload["reason"])
	require.Equal(t, "v1", payload["policy_version"])
	require.Equal(t, "abcd1234", payload["subject_sha256_8"])
	require.Equal(t, "k8s__get_logs", payload["tool_name"])
}

func TestSyslogAuditSink_EmitPropagatesWriterError(t *testing.T) {
	fw := &fakeSyslogWriter{writeErr: errors.New("boom")}
	sink := &SyslogAuditSink{writer: fw}
	err := sink.Emit(context.Background(), AuditRecord{
		Outcome: "deny",
		Reason:  "not_in_allow_list",
	})
	require.Error(t, err)
	require.EqualError(t, err, "boom")
}

func TestSyslogAuditSink_CloseDelegatesWriter(t *testing.T) {
	fw := &fakeSyslogWriter{}
	sink := &SyslogAuditSink{writer: fw}
	require.NoError(t, sink.Close())
	require.True(t, fw.closed)
}
