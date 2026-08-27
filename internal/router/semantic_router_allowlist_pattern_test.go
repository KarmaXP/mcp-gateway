package router

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func routerWithTwoTools(t *testing.T) *SemanticRouter {
	t.Helper()
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	echo := index.Tool{Name: "alpha__echo", Description: "repeat text back"}
	logs := index.Tool{Name: "beta__logs", Description: "read pod logs"}
	emb.vecs[index.FormatDocument(echo)] = []float32{1, 0, 0, 0}
	emb.vecs[index.FormatDocument(logs)] = []float32{0, 1, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	sr := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, sr, context.Background(), "v1", []CatalogEntry{
		{Tool: echo, UpstreamID: "b1"},
		{Tool: logs, UpstreamID: "b2"},
	})
	return sr
}

func TestExactResolutionHonoursAPatternAllowList(t *testing.T) {
	tests := []struct {
		name      string
		allowList []string
		tool      string
		wantTool  string
	}{
		{name: "the documented wildcard grants the whole catalog", allowList: []string{"*"}, tool: "alpha__echo", wantTool: "alpha__echo"},
		{name: "a prefix pattern grants its own upstream", allowList: []string{"alpha__*"}, tool: "alpha__echo", wantTool: "alpha__echo"},
		{name: "an explicit entry still works", allowList: []string{"alpha__echo"}, tool: "alpha__echo", wantTool: "alpha__echo"},
		{name: "a single-character wildcard matches", allowList: []string{"alpha__ech?"}, tool: "alpha__echo", wantTool: "alpha__echo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sr := routerWithTwoTools(t)

			got, dec, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{
				ToolName:       tc.tool,
				AllowList:      tc.allowList,
				AllowListAuthz: AllowListAuthzRestricted,
				CatalogVersion: "v1",
			})

			require.NoError(t, err,
				"a pattern allow-list left the router with no candidates for a tool it holds in its catalog")
			require.Equal(t, tc.wantTool, got)
			require.Equal(t, "exact", dec.FallbackLayer)
		})
	}
}

func TestAPatternThatMatchesNothingDeniesRatherThanOpeningUp(t *testing.T) {
	sr := routerWithTwoTools(t)

	_, _, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:       "alpha__echo",
		AllowList:      []string{"zzz__*"},
		AllowListAuthz: AllowListAuthzRestricted,
		CatalogVersion: "v1",
	})

	require.Error(t, err,
		"an allow-list matching no catalogued tool must not resolve anything")
}

func TestAPatternDoesNotReachAnotherUpstreamsTool(t *testing.T) {
	sr := routerWithTwoTools(t)

	_, _, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:       "beta__logs",
		AllowList:      []string{"alpha__*"},
		AllowListAuthz: AllowListAuthzRestricted,
		CatalogVersion: "v1",
	})

	require.Error(t, err, "alpha__* must not resolve a tool belonging to beta")
}

func TestExpandAllowedToolNamesGivesTheStoreConcreteNames(t *testing.T) {
	sr := routerWithTwoTools(t)

	require.ElementsMatch(t, []string{"alpha__echo", "beta__logs"}, sr.expandAllowedToolNames([]string{"*"}))
	require.Equal(t, []string{"alpha__echo"}, sr.expandAllowedToolNames([]string{"alpha__*"}))
	require.Empty(t, sr.expandAllowedToolNames([]string{"zzz__*"}))
	require.Equal(t, []string{"alpha__echo"}, sr.expandAllowedToolNames([]string{"alpha__echo"}),
		"a list with no pattern must reach the store unchanged")
	require.Nil(t, sr.expandAllowedToolNames(nil))
}

func TestVectorResolutionHonoursAPatternAllowList(t *testing.T) {
	dim := 4
	st := store.NewInMemoryVectorStore(dim)
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	echo := index.Tool{Name: "alpha__echo", Description: "repeat text back"}
	emb.vecs[index.FormatDocument(echo)] = []float32{1, 0, 0, 0}

	const vagueName = "alpha__say_it_again"
	const intent = "repeat the user text"
	emb.vecs[index.FormatQuery(vagueName, intent, nil)] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.AllowAutoRename = true
	sr := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, sr, context.Background(), "v1", []CatalogEntry{{Tool: echo, UpstreamID: "b1"}})

	got, dec, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:       vagueName,
		IntentText:     intent,
		AllowList:      []string{"*"},
		AllowListAuthz: AllowListAuthzRestricted,
		CatalogVersion: "v1",
	})

	require.NoError(t, err,
		"the vector store was handed the raw pattern as a tool name, so no indexed tool could match it")
	require.Equal(t, "alpha__echo", got)
	require.Equal(t, "vector", dec.FallbackLayer)
}

func TestRouterAndPolicyAgreeOnWhatAnAllowListEntryMeans(t *testing.T) {
	sr := routerWithTwoTools(t)
	entries := [][]string{
		{"*"},
		{"alpha__*"},
		{"*__echo"},
		{"alpha__ech?"},
		{"alpha__echo"},
		{"beta__logs"},
		{"zzz__*"},
		{"alpha__*", "beta__logs"},
	}
	for _, tool := range []string{"alpha__echo", "beta__logs"} {
		for _, entry := range entries {
			t.Run(tool+" vs "+strings.Join(entry, ","), func(t *testing.T) {
				want, err := policy.AllowListPermits(tool, entry)
				require.NoError(t, err)

				require.Equal(t, want, sr.allowed(tool, entry, false),
					"the router and the policy layer disagree on %q for %v, which is how a wildcard "+
						"allow-list once left every tools/call unroutable", tool, entry)
			})
		}
	}
}
