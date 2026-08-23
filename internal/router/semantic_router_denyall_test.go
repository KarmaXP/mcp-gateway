package router

import (
	"context"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func TestResolveToolsCallDenyAllSkipsVectorSearch(t *testing.T) {
	dim := 4
	inner := store.NewInMemoryVectorStore(dim)
	st := &queryCountingStore{inner: inner}
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	aws := index.ToolRow{Name: "aws__list_buckets", Description: "s3 buckets"}
	doc := index.FormatDocument(aws)
	emb.vecs[doc] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	sr := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, sr, context.Background(), "v1", []IndexedTool{
		{ToolRow: aws, UpstreamID: "b1"},
	})

	_, dec, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:       "aws__list_buckets",
		AllowListAuthz: AllowListAuthzDenyAll,
	})
	require.NoError(t, err)
	require.Equal(t, OutcomeNone, dec.Outcome)
	require.Equal(t, "authz", dec.FallbackLayer)
	require.Zero(t, st.queries)
}

func TestFilterToolsForListDenyAllSkipsEmbed(t *testing.T) {
	dim := 4
	embedCalls := 0
	emb := &embedInvokeCounter{
		inner: &mapEmbed{vecs: make(map[string][]float32), dim: dim},
	}
	st := store.NewInMemoryVectorStore(dim)
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.FilterList
	sr := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, sr, context.Background(), "v1", []IndexedTool{
		{ToolRow: index.ToolRow{Name: "p__echo", Description: "echo"}, UpstreamID: "b1"},
	})
	emb.after = func() { embedCalls++ }

	keep, useFull := sr.FilterToolsForList(context.Background(), RoutingSignal{
		IntentText:     "want echo",
		AllowListAuthz: AllowListAuthzDenyAll,
		CatalogVersion: "v1",
	})
	require.False(t, useFull)
	require.Empty(t, keep)
	require.Zero(t, embedCalls)
}

type embedInvokeCounter struct {
	inner *mapEmbed
	after func()
}

func (c *embedInvokeCounter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if c.after != nil {
		c.after()
	}
	return c.inner.Embed(ctx, texts)
}
