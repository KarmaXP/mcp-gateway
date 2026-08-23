package router

import (
	"context"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

type fixedEmbed struct {
	dim int
}

func (f fixedEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, 0, len(texts))
	for range texts {
		v := make([]float32, f.dim)
		v[0] = 1
		store.L2Normalize(v)
		out = append(out, v)
	}
	return out, nil
}

func TestReindexDeletesPreviousCatalogVersion(t *testing.T) {
	ctx := context.Background()
	st := store.NewInMemoryVectorStore(4)
	emb := fixedEmbed{dim: 4}
	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.ScoreMin = 0.01
	cfg.TopK = 8
	sr := NewSemanticRouter(cfg, emb, st, 4)

	toolsV1 := []IndexedTool{{ToolRow: index.ToolRow{Name: "a__one", Description: "one"}, UpstreamID: "b1"}}
	reindexAndApply(t, sr, ctx, "v1", toolsV1)

	toolsV2 := []IndexedTool{{ToolRow: index.ToolRow{Name: "a__two", Description: "two"}, UpstreamID: "b1"}}
	reindexAndApply(t, sr, ctx, "v2", toolsV2)

	hits, err := st.Query(ctx, []float32{1, 0, 0, 0}, 4, store.VectorSearchFilter{CatalogVersion: "v1"})
	require.NoError(t, err)
	require.Empty(t, hits)

	hits, err = st.Query(ctx, []float32{1, 0, 0, 0}, 4, store.VectorSearchFilter{CatalogVersion: "v2"})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}
