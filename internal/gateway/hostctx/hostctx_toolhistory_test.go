package hostctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithMCPSessionIDRoundTrip(t *testing.T) {
	ctx := WithMCPSessionID(context.Background(), "  sid-1  ")
	require.Equal(t, "sid-1", MCPSessionIDFromContext(ctx))
	require.Equal(t, "", MCPSessionIDFromContext(context.Background()))
	ctx2 := WithMCPSessionID(context.Background(), "   ")
	require.Equal(t, "", MCPSessionIDFromContext(ctx2))
}

type recorderSpy struct {
	names []string
}

func (r *recorderSpy) RecordSuccessfulToolCall(namespaced string) {
	r.names = append(r.names, namespaced)
}

func TestToolCallRecorderRoundTrip(t *testing.T) {
	var spy recorderSpy
	ctx := WithToolCallRecorder(context.Background(), &spy)
	RecordSuccessfulToolCall(ctx, "a__t1")
	RecordSuccessfulToolCall(ctx, "")
	require.Equal(t, []string{"a__t1"}, spy.names)

	ctxNil := WithToolCallRecorder(context.Background(), nil)
	RecordSuccessfulToolCall(ctxNil, "b__x")
	require.Equal(t, []string{"a__t1"}, spy.names)
}

func TestRecentToolNamesRoundTrip(t *testing.T) {
	ctx := WithRecentToolNames(context.Background(), []string{"p__a", "p__b"})
	got := RecentToolNamesFromContext(ctx)
	require.Equal(t, []string{"p__a", "p__b"}, got)
	cp := RecentToolNamesFromContext(ctx)
	cp[0] = "mutated"
	require.Equal(t, "p__a", RecentToolNamesFromContext(ctx)[0])

	require.Nil(t, RecentToolNamesFromContext(context.Background()))
	ctxEmpty := WithRecentToolNames(context.Background(), nil)
	require.Nil(t, RecentToolNamesFromContext(ctxEmpty))
}
