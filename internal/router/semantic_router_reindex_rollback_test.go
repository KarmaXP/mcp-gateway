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

type failUpsertStore struct {
	inner           *store.InMemoryVectorStore
	deletedVersions []string
	upsertFailAfter int
	upsertCalls     int
}

func (f *failUpsertStore) Upsert(ctx context.Context, records []store.ToolVectorRecord) error {
	f.upsertCalls++
	if f.upsertFailAfter > 0 && f.upsertCalls >= f.upsertFailAfter {
		return errors.New("upsert failed")
	}
	return f.inner.Upsert(ctx, records)
}

func (f *failUpsertStore) Query(ctx context.Context, vector []float32, topK int, filter store.VectorSearchFilter) ([]store.VectorSearchHit, error) {
	return f.inner.Query(ctx, vector, topK, filter)
}

func (f *failUpsertStore) DeleteCatalogVersion(ctx context.Context, version string) error {
	f.deletedVersions = append(f.deletedVersions, version)
	return f.inner.DeleteCatalogVersion(ctx, version)
}

func TestReindexRollsBackNewCatalogVersionOnUpsertFailure(t *testing.T) {
	ctx := context.Background()
	st := &failUpsertStore{inner: store.NewInMemoryVectorStore(4), upsertFailAfter: 1}
	emb := fixedEmbed{dim: 4}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	sr := NewSemanticRouter(cfg, emb, st, 4)

	tools := []IndexedTool{{ToolRow: index.ToolRow{Name: "a__one", Description: "one"}, UpstreamID: "b1"}}
	err := sr.Reindex(ctx, "v-fail", tools)
	require.Error(t, err)
	require.Contains(t, st.deletedVersions, "v-fail")
	require.Equal(t, "", sr.CatalogVersion())
}
