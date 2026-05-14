package eval

import (
	"math"
	"sort"

	"github.com/KarmaXP/mcp-gateway/internal/router"
)

const (
	defaultRankCutoff = 5
	ndcgGainBase = 2.0
	ndcgLogRankBase = 2 // 1-based rank i uses denominator log2(i+ndcgLogRankBase)
)

func ReciprocalRankAtK(candidates []router.ScoredTool, relevance map[string]float64, k int) float64 {
	maxRel := 0.0
	for _, rel := range relevance {
		if rel > maxRel {
			maxRel = rel
		}
	}
	if maxRel <= 0 {
		return 0
	}
	limit := rankLimit(k, len(candidates))
	for i := 0; i < limit; i++ {
		if relevance[candidates[i].Name] >= maxRel {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

func NDCGAtK(candidates []router.ScoredTool, relevance map[string]float64, k int) float64 {
	if len(relevance) == 0 {
		return 0
	}
	limit := rankLimit(k, len(candidates))
	if limit == 0 {
		return 0
	}

	dcg := 0.0
	for i := 0; i < limit; i++ {
		rel := relevance[candidates[i].Name]
		if rel <= 0 {
			continue
		}
		dcg += (math.Pow(ndcgGainBase, rel) - 1) / math.Log2(float64(i+ndcgLogRankBase))
	}

	ideal := make([]float64, 0, len(relevance))
	for _, rel := range relevance {
		if rel > 0 {
			ideal = append(ideal, rel)
		}
	}
	if len(ideal) == 0 {
		return 0
	}
	sort.Slice(ideal, func(i, j int) bool { return ideal[i] > ideal[j] })

	idcg := 0.0
	idealLimit := rankLimit(k, len(ideal))
	for i := 0; i < idealLimit; i++ {
		idcg += (math.Pow(ndcgGainBase, ideal[i]) - 1) / math.Log2(float64(i+ndcgLogRankBase))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func MeanReciprocalRankAtK(rankings [][]router.ScoredTool, relevances []map[string]float64, k int) float64 {
	n := min(len(rankings), len(relevances))
	if n == 0 {
		return 0
	}
	total := 0.0
	for i := 0; i < n; i++ {
		total += ReciprocalRankAtK(rankings[i], relevances[i], k)
	}
	return total / float64(n)
}

func MeanNDCGAtK(rankings [][]router.ScoredTool, relevances []map[string]float64, k int) float64 {
	n := min(len(rankings), len(relevances))
	if n == 0 {
		return 0
	}
	total := 0.0
	for i := 0; i < n; i++ {
		total += NDCGAtK(rankings[i], relevances[i], k)
	}
	return total / float64(n)
}

func rankLimit(k, n int) int {
	if k <= 0 {
		k = defaultRankCutoff
	}
	return min(k, n)
}
