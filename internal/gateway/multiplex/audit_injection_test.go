package multiplex

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type recordingAuditSink struct {
	mu      sync.Mutex
	records []policy.AuditRecord
}

func (s *recordingAuditSink) Emit(_ context.Context, rec policy.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

func (s *recordingAuditSink) all() []policy.AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]policy.AuditRecord(nil), s.records...)
}

func TestDeniedToolCallReachesTheInjectedAuditor(t *testing.T) {
	t.Parallel()
	sink := &recordingAuditSink{}
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "list"})
	a, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), WithAuditor(policy.NewAuditor(sink, []byte("test-pepper"))))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__echo"})
	ctx = hostctx.WithSubjectID(ctx, "user-7")
	ctx = hostctx.WithPolicyVersion(ctx, "v-audit")
	params, _ := json.Marshal(map[string]any{"name": "alpha__list", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(ctx, json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)

	records := sink.all()
	require.Len(t, records, 1)
	require.Equal(t, "deny", records[0].Outcome)
	require.Equal(t, "not_in_allow_list", records[0].Reason)
	require.Equal(t, "alpha__list", records[0].ToolName)
	require.Equal(t, "v-audit", records[0].PolicyVersion)
	require.Len(t, records[0].SubjectSHA256, 16)
	require.NotContains(t, records[0].SubjectSHA256, "user-7")
}

func TestTwoMultiplexersDoNotShareAnAuditor(t *testing.T) {
	t.Parallel()
	first, second := &recordingAuditSink{}, &recordingAuditSink{}
	deny := func(sink *recordingAuditSink, tool string) {
		b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo", "list"})
		a, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), WithAuditor(policy.NewAuditor(sink, []byte("test-pepper"))))
		require.NoError(t, err)
		_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
		_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))
		ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__nothing"})
		params, _ := json.Marshal(map[string]any{"name": tool, "arguments": map[string]any{}})
		_, err = a.ToolsCall(ctx, json.RawMessage(`3`), params)
		require.NoError(t, err)
	}
	deny(first, "alpha__echo")
	deny(second, "alpha__list")

	firstRecords, secondRecords := first.all(), second.all()
	require.Len(t, firstRecords, 1)
	require.Len(t, secondRecords, 1)
	require.Equal(t, "alpha__echo", firstRecords[0].ToolName)
	require.Equal(t, "alpha__list", secondRecords[0].ToolName)
}

func TestAllowedToolCallIsAudited(t *testing.T) {
	t.Parallel()
	sink := &recordingAuditSink{}
	b1 := mock.NewMockUpstream("b1", "alpha", []string{"echo"})
	a, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), WithAuditor(policy.NewAuditor(sink, []byte("test-pepper"))))
	require.NoError(t, err)
	_, _ = a.Initialize(context.Background(), json.RawMessage(`1`))
	_, _ = a.ToolsList(context.Background(), json.RawMessage(`2`))

	ctx := hostctx.WithAllowedToolNames(context.Background(), []string{"alpha__echo"})
	ctx = hostctx.WithSubjectID(ctx, "user-7")
	params, _ := json.Marshal(map[string]any{"name": "alpha__echo", "arguments": map[string]any{}})
	resp, err := a.ToolsCall(ctx, json.RawMessage(`3`), params)
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	records := sink.all()
	require.Len(t, records, 1, "an audit trail of denials only cannot say who called what successfully")
	require.Equal(t, "allow", records[0].Outcome)
	require.Equal(t, "allow_list_match", records[0].Reason)
	require.Equal(t, "alpha__echo", records[0].ToolName)
}

func TestSubjectPseudonymIsKeyedByThePepper(t *testing.T) {
	t.Parallel()
	pseudonymFor := func(pepper string) string {
		sink := &recordingAuditSink{}
		policy.NewAuditor(sink, []byte(pepper)).Record(context.Background(), policy.Decision{
			Outcome:   "deny",
			SubjectID: "user@example.com",
		})
		return sink.all()[0].SubjectSHA256
	}
	first, second := pseudonymFor("pepper-a"), pseudonymFor("pepper-b")
	require.Len(t, first, 16)
	require.NotEqual(t, first, second, "without a pepper the same subject hashes the same in every deployment")
}
