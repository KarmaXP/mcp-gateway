package eval

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/rules"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// TestPhase2SyntheticCatalogSize documents plan §B.8 minimum catalog scale.
func TestPhase2SyntheticCatalogSize(t *testing.T) {
	require.GreaterOrEqual(t, len(SyntheticCatalog()), 20, "Phase 2 acceptance expects ≥20 synthetic tools")
}

// TestPhase2VectorRecallLexical runs a reproducible recall@1 check using LexicalEmbedder (no live ONNX/Qdrant).
func TestPhase2VectorRecallLexical(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewMemory(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	eng := router.NewEngine(cfg, emb, st, dim)

	cat := SyntheticCatalog()
	require.NoError(t, eng.Reindex(ctx, "eval-v1", cat))
	ver := eng.CatalogVersion()
	require.Equal(t, "eval-v1", ver)

	cases := GoldenCases()
	hits := 0
	for _, tc := range cases {
		sig := router.RoutingSignal{
			ToolName:       "wrong__tool_name",
			IntentText:     tc.Intent,
			CatalogVersion: ver,
			AllowedTools:   tc.Allowed,
		}
		got, dec, err := eng.ResolveToolsCall(ctx, sig)
		require.NoError(t, err, "case %s", tc.WantTool)
		require.Equal(t, router.OutcomeVectorHit, dec.Outcome)
		if got == tc.WantTool {
			hits++
		} else {
			t.Logf("miss: want %s got %s candidates=%v", tc.WantTool, got, dec.Candidates)
		}
	}
	recall := float64(hits) / float64(len(cases))
	t.Logf("recall@1 lexical embed: %.3f (%d/%d)", recall, hits, len(cases))
	require.GreaterOrEqual(t, recall, 0.95, "golden set should resolve with lexical embed for benchmark harness")
}

// TestPhase2EmbedAndQueryP95 measures end-to-end router latency for the vector path (embed + search).
func TestPhase2EmbedAndQueryP95(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewMemory(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	eng := router.NewEngine(cfg, emb, st, dim)
	require.NoError(t, eng.Reindex(ctx, "eval-v1", SyntheticCatalog()))
	ver := eng.CatalogVersion()

	cases := GoldenCases()
	const rounds = 30
	lat := make([]time.Duration, 0, rounds*len(cases))
	for r := 0; r < rounds; r++ {
		for _, tc := range cases {
			start := time.Now()
			_, _, err := eng.ResolveToolsCall(ctx, router.RoutingSignal{
				ToolName:       "wrong__tool_name",
				IntentText:     tc.Intent,
				CatalogVersion: ver,
			})
			require.NoError(t, err)
			lat = append(lat, time.Since(start))
		}
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p95 := lat[len(lat)*95/100]
	t.Logf("p95 router latency (embed+query, in-memory store, N=%d): %s", len(lat), p95)
	// Budget is environment-specific; cap only absurd regressions on in-memory path.
	require.Less(t, p95, 500*time.Millisecond)
}

// TestPhase2SiloNarrowingRespectsAllowedTools ensures silo keywords shrink the candidate set (plan §B.8).
func TestPhase2SiloNarrowingRespectsAllowedTools(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewMemory(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	eng := router.NewEngine(cfg, emb, st, dim)
	eng.SetRules(rules.New(nil, map[string]string{"kubernetes": "k8s"}))
	require.NoError(t, eng.Reindex(ctx, "eval-v2", SyntheticCatalog()))
	ver := eng.CatalogVersion()

	sig := router.RoutingSignal{
		ToolName:       "wrong__tool",
		IntentText:     "debug kubernetes pod logs during incident",
		CatalogVersion: ver,
		AllowedTools:   []string{"k8s__get_logs", "aws__list_buckets"},
	}
	got, dec, err := eng.ResolveToolsCall(ctx, sig)
	require.NoError(t, err)
	require.Equal(t, "k8s__get_logs", got)
	require.Equal(t, router.OutcomeVectorHit, dec.Outcome)
}
