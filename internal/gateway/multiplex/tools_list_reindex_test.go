package multiplex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/mock"
)

type countingEmbed struct {
	calls atomic.Int32
	dim   int
}

func (c *countingEmbed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	c.calls.Add(1)
	out := make([][]float32, 0, len(texts))
	for range texts {
		v := make([]float32, c.dim)
		v[0] = 1
		store.L2Normalize(v)
		out = append(out, v)
	}
	return out, nil
}

func TestToolsListSkipsSemanticReindexWhenCatalogUnchanged(t *testing.T) {
	b1 := mock.NewMockUpstream("b1", "p", []string{"echo"})
	emb := &countingEmbed{dim: 4}
	rcfg := router.DefaultSemanticRouterRuntimeConfig()
	rcfg.Mode = mode.AssistList
	rcfg.ScoreMin = 0.01
	rcfg.TopK = 8
	sr := router.NewSemanticRouter(rcfg, emb, store.NewInMemoryVectorStore(4), 4)

	a, err := New(context.Background(), []upstream.Client{b1}, WithListTTL(0), WithSemanticRouter(sr))
	require.NoError(t, err)

	_, err = a.ToolsList(context.Background(), json.RawMessage(`1`))
	require.NoError(t, err)
	afterFirst := emb.calls.Load()
	require.Greater(t, afterFirst, int32(0))

	_, err = a.ToolsList(context.Background(), json.RawMessage(`2`))
	require.NoError(t, err)
	require.Equal(t, afterFirst, emb.calls.Load(), "unchanged catalog should not re-embed")
}
