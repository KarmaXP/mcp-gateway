package router

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// errStore delegates Upsert then fails Query (simulates Qdrant timeout / unavailable).
type errStore struct {
	inner *store.Memory
	err   error
}

func (e *errStore) Upsert(ctx context.Context, points []store.Point) error {
	return e.inner.Upsert(ctx, points)
}

func (e *errStore) Query(ctx context.Context, vector []float32, topK int, filter store.Filter) ([]store.Result, error) {
	_ = ctx
	_ = vector
	_ = topK
	_ = filter
	return nil, e.err
}

func (e *errStore) DeleteCatalogVersion(ctx context.Context, version string) error {
	return e.inner.DeleteCatalogVersion(ctx, version)
}

func TestEngineStoreQueryFails(t *testing.T) {
	dim := 4
	mem := store.NewMemory(dim)
	st := &errStore{inner: mem, err: errors.New("vector backend unavailable")}
	emb := &mapEmbed{vecs: make(map[string][]float32), dim: dim}
	row := index.ToolRow{Name: "p__echo", Description: "e"}
	doc := index.FormatDocument(row)
	emb.vecs[doc] = []float32{1, 0, 0, 0}
	q := index.FormatQuery("typo", "", nil)
	emb.vecs[q] = []float32{1, 0, 0, 0}

	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.TopK = 4
	cfg.ScoreMin = 0.5
	e := NewEngine(cfg, emb, st, dim)
	require.NoError(t, e.Reindex(context.Background(), "v1", []CatalogEntry{{ToolRow: row, BackendID: "b1"}}))

	_, dec, err := e.ResolveToolsCall(context.Background(), RoutingSignal{ToolName: "typo"})
	require.Error(t, err)
	require.NotNil(t, dec)
	require.Equal(t, OutcomeMissStoreError, dec.Outcome)
}
