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

func TestRouterEvalSyntheticCatalogSize(t *testing.T) {
	require.GreaterOrEqual(t, len(SyntheticCatalog()), 20, "router eval harness expects ≥20 synthetic tools")
}

func TestRouterEvalVectorRecallLexical(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewInMemoryVectorStore(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	sr := router.NewSemanticRouter(cfg, emb, st, dim)

	cat := SyntheticCatalog()
	require.NoError(t, sr.Reindex(ctx, "eval-v1", cat))
	ver := sr.CatalogVersion()
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
		got, dec, err := sr.ResolveToolsCall(ctx, sig)
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

func TestRouterEvalEmbedAndQueryP95(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewInMemoryVectorStore(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	sr := router.NewSemanticRouter(cfg, emb, st, dim)
	require.NoError(t, sr.Reindex(ctx, "eval-v1", SyntheticCatalog()))
	ver := sr.CatalogVersion()

	cases := GoldenCases()
	const rounds = 30
	lat := make([]time.Duration, 0, rounds*len(cases))
	for r := 0; r < rounds; r++ {
		for _, tc := range cases {
			start := time.Now()
			_, _, err := sr.ResolveToolsCall(ctx, router.RoutingSignal{
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
	require.Less(t, p95, 500*time.Millisecond)
}

func TestRouterEvalSiloNarrowingRespectsAllowedTools(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewInMemoryVectorStore(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	sr := router.NewSemanticRouter(cfg, emb, st, dim)
	sr.SetRules(rules.New(nil, map[string]string{"kubernetes": "k8s"}))
	require.NoError(t, sr.Reindex(ctx, "eval-v2", SyntheticCatalog()))
	ver := sr.CatalogVersion()

	sig := router.RoutingSignal{
		ToolName:       "wrong__tool",
		IntentText:     "debug kubernetes pod logs during incident",
		CatalogVersion: ver,
		AllowedTools:   []string{"k8s__get_pod_logs", "prom__query_range"},
	}
	got, dec, err := sr.ResolveToolsCall(ctx, sig)
	require.NoError(t, err)
	require.Equal(t, "k8s__get_pod_logs", got)
	require.Equal(t, router.OutcomeVectorHit, dec.Outcome)
}

func TestGoldenCasesMRRAndNDCG(t *testing.T) {
	ctx := context.Background()
	dim := 384
	st := store.NewInMemoryVectorStore(dim)
	emb := LexicalEmbedder{Dim: dim}
	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = 16
	cfg.ScoreMin = 0.08
	cfg.AllowAutoRename = true
	sr := router.NewSemanticRouter(cfg, emb, st, dim)

	require.NoError(t, sr.Reindex(ctx, "eval-v1", SyntheticCatalog()))
	ver := sr.CatalogVersion()
	cases := GoldenCases()

	rankings := make([][]router.ScoredTool, 0, len(cases))
	relevances := make([]map[string]float64, 0, len(cases))

	for _, tc := range cases {
		got, dec, err := sr.ResolveToolsCall(ctx, router.RoutingSignal{
			ToolName:       "wrong__tool_name",
			IntentText:     tc.Intent,
			CatalogVersion: ver,
			AllowedTools:   tc.Allowed,
		})
		require.NoError(t, err, "case %s", tc.WantTool)
		require.Equal(t, router.OutcomeVectorHit, dec.Outcome)

		candidates := append([]router.ScoredTool(nil), dec.Candidates...)
		if len(candidates) == 0 {
			candidates = append(candidates, router.ScoredTool{
				Name:   got,
				Score:  dec.Confidence,
				Source: "vector",
			})
		}
		rankings = append(rankings, candidates)
		relevances = append(relevances, tc.Relevance)
	}

	mrr := MeanReciprocalRankAtK(rankings, relevances, 5)
	ndcg5 := MeanNDCGAtK(rankings, relevances, 5)
	t.Logf("golden metrics lexical baseline: MRR=%.3f nDCG@5=%.3f", mrr, ndcg5)
	require.GreaterOrEqual(t, mrr, 0.95)
	require.GreaterOrEqual(t, ndcg5, 0.90)
}
