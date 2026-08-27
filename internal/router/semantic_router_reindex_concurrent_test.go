package router

import (
	"context"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

type countingEmbed struct {
	inner     fixedEmbed
	inFlight  atomic.Int32
	maxFlight atomic.Int32
	delay     time.Duration
}

func (c *countingEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	cur := c.inFlight.Add(1)
	for {
		prev := c.maxFlight.Load()
		if cur <= prev || c.maxFlight.CompareAndSwap(prev, cur) {
			break
		}
	}
	time.Sleep(c.delay)
	defer c.inFlight.Add(-1)
	return c.inner.Embed(ctx, texts)
}

func TestConcurrentReindexSerialized(t *testing.T) {
	ctx := context.Background()
	st := store.NewInMemoryVectorStore(4)
	emb := &countingEmbed{inner: fixedEmbed{dim: 4}, delay: 25 * time.Millisecond}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	sr := NewSemanticRouter(cfg, emb, st, 4)

	toolsA := []CatalogEntry{{Tool: index.Tool{Name: "a__one", Description: "one"}, UpstreamID: "b1"}}
	toolsB := []CatalogEntry{{Tool: index.Tool{Name: "a__two", Description: "two"}, UpstreamID: "b1"}}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		errs <- sr.Reindex(ctx, "v-a", toolsA)
	}()
	go func() {
		defer wg.Done()
		errs <- sr.Reindex(ctx, "v-b", toolsB)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.LessOrEqual(t, emb.maxFlight.Load(), int32(1), "concurrent Reindex must not overlap embed/upsert")
}

func TestReindexDoesNotExposeCatalogVersionUntilApplyCatalog(t *testing.T) {
	ctx := context.Background()
	st := store.NewInMemoryVectorStore(4)
	emb := fixedEmbed{dim: 4}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	sr := NewSemanticRouter(cfg, emb, st, 4)

	toolsV1 := []CatalogEntry{{Tool: index.Tool{Name: "a__one", Description: "one"}, UpstreamID: "b1"}}
	reindexAndApply(t, sr, ctx, "v1", toolsV1)
	require.Equal(t, "v1", sr.CatalogVersion())

	toolsV2 := []CatalogEntry{{Tool: index.Tool{Name: "a__two", Description: "two"}, UpstreamID: "b1"}}
	require.NoError(t, sr.Reindex(ctx, "v2", toolsV2))
	require.Equal(t, "v1", sr.CatalogVersion(), "in-memory catalog must stay at v1 until ApplyCatalog")

	hits, err := st.Query(ctx, []float32{1, 0, 0, 0}, 4, store.VectorSearchFilter{CatalogVersion: "v2"})
	require.NoError(t, err)
	require.NotEmpty(t, hits, "vector store may already hold v2 after Reindex")

	sr.ApplyCatalog(ctx, "v2", toolsV2)
	require.Equal(t, "v2", sr.CatalogVersion())
}

func TestConcurrentReindexAndApplyCatalogUnderRace(t *testing.T) {
	ctx := context.Background()
	st := store.NewInMemoryVectorStore(4)
	emb := fixedEmbed{dim: 4}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	sr := NewSemanticRouter(cfg, emb, st, 4)

	toolsV1 := []CatalogEntry{{Tool: index.Tool{Name: "a__one", Description: "one"}, UpstreamID: "b1"}}
	reindexAndApply(t, sr, ctx, "v1", toolsV1)

	toolsV2 := []CatalogEntry{{Tool: index.Tool{Name: "a__two", Description: "two"}, UpstreamID: "b1"}}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		errs <- sr.Reindex(ctx, "v2", toolsV2)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		sr.ApplyCatalog(ctx, "v2", toolsV2)
		errs <- nil
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, "v2", sr.CatalogVersion())
}
