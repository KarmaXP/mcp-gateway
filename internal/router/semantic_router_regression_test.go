package router

import (
	"context"
	"errors"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

type queryCountingStore struct {
	inner   *store.InMemoryVectorStore
	queries int
}

func (q *queryCountingStore) Upsert(ctx context.Context, records []store.ToolVectorRecord) error {
	return q.inner.Upsert(ctx, records)
}

func (q *queryCountingStore) Query(ctx context.Context, vector []float32, topK int, filter store.VectorSearchFilter) ([]store.VectorSearchHit, error) {
	if filter.BlocksAllTools() {
		return nil, nil
	}
	q.queries++
	return q.inner.Query(ctx, vector, topK, filter)
}

func (q *queryCountingStore) DeleteCatalogVersion(ctx context.Context, version string) error {
	return q.inner.DeleteCatalogVersion(ctx, version)
}

func TestResolveToolsCallSiloNarrowedZeroToolsSkipsVectorSearch(t *testing.T) {
	dim := 4
	inner := store.NewInMemoryVectorStore(dim)
	st := &queryCountingStore{inner: inner}
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}

	aws := index.ToolRow{Name: "aws__list_buckets", Description: "s3 buckets", ParamKeys: nil}
	doc := index.FormatDocument(aws)
	emb.vecs[doc] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.5
	sr := NewSemanticRouter(cfg, emb, st, dim)
	sr.SetRules(rules.New(nil, map[string]string{"kubernetes": "k8s"}))
	reindexAndApply(t, sr, context.Background(), "v1", []IndexedTool{
		{ToolRow: aws, UpstreamID: "b1"},
	})

	_, dec, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{
		ToolName:   "wrong__tool",
		IntentText: "fetch kubernetes pod logs",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoCandidates))
	require.Equal(t, OutcomeMissNoCandidates, dec.Outcome)
	require.Zero(t, st.queries, "narrowed empty allow-list must not run unrestricted vector search")
}
