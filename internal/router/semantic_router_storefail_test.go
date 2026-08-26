package router

import (
	"context"
	"errors"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

const (
	testVectorDim = 4
	testRouterTopK = 4
	testRouterMinHit = 0.5
)

type errStore struct {
	inner *store.InMemoryVectorStore
	err   error
}

func (e *errStore) Upsert(ctx context.Context, records []store.ToolVectorRecord) error {
	return e.inner.Upsert(ctx, records)
}

func (e *errStore) Query(ctx context.Context, vector []float32, topK int, filter store.VectorSearchFilter) ([]store.VectorSearchHit, error) {
	_ = ctx
	_ = vector
	_ = topK
	_ = filter
	return nil, e.err
}

func (e *errStore) DeleteCatalogVersion(ctx context.Context, version string) error {
	return e.inner.DeleteCatalogVersion(ctx, version)
}

func TestSemanticRouterStoreQueryFails(t *testing.T) {
	dim := testVectorDim
	mem := store.NewInMemoryVectorStore(dim)
	st := &errStore{inner: mem, err: errors.New("vector store unavailable")}
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	row := index.ToolRow{Name: "p__echo", Description: "e"}
	doc := index.FormatDocument(row)
	emb.vecs[doc] = []float32{1, 0, 0, 0}
	q := index.FormatQuery("typo", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.TopK = testRouterTopK
	cfg.ScoreMin = testRouterMinHit
	sr := NewSemanticRouter(cfg, emb, st, dim)
	reindexAndApply(t, sr, context.Background(), "v1", []IndexedTool{{ToolRow: row, UpstreamID: "b1"}})

	_, dec, err := sr.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "typo"})
	require.Error(t, err)
	require.NotNil(t, dec)
	require.Equal(t, OutcomeMissStoreError, dec.Outcome)
}
