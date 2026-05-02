package store

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

const (
	cosineSimilarityMax = 1.0
	cosineSimilarityMin = -1.0
)

// InMemoryVectorStore is a cosine-similarity vector index for tests and small deployments.
type InMemoryVectorStore struct {
	dim int

	mu      sync.RWMutex
	records []ToolVectorRecord
}

func NewInMemoryVectorStore(dim int) *InMemoryVectorStore {
	return &InMemoryVectorStore{dim: dim}
}

func (m *InMemoryVectorStore) Upsert(ctx context.Context, records []ToolVectorRecord) error {
	_ = ctx
	for _, p := range records {
		if len(p.Vector) != m.dim {
			return ErrDimensionMismatch
		}
	}
	m.mu.Lock()
	m.records = append([]ToolVectorRecord(nil), records...)
	m.mu.Unlock()
	return nil
}

func (m *InMemoryVectorStore) DeleteCatalogVersion(ctx context.Context, version string) error {
	_ = ctx
	m.mu.Lock()
	keep := m.records[:0]
	for _, p := range m.records {
		if p.CatalogVersion != version {
			keep = append(keep, p)
		}
	}
	m.records = keep
	m.mu.Unlock()
	return nil
}

func (m *InMemoryVectorStore) Query(ctx context.Context, vector []float32, topK int, filter VectorSearchFilter) ([]VectorSearchHit, error) {
	_ = ctx
	if len(vector) != m.dim {
		return nil, ErrDimensionMismatch
	}
	if topK <= 0 {
		topK = defaults.DefaultVectorSearchTopK
	}
	allow := map[string]struct{}{}
	useAllow := len(filter.AllowedToolNames) > 0
	for _, n := range filter.AllowedToolNames {
		allow[n] = struct{}{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	type scored struct {
		res   VectorSearchHit
		score float64
	}
	var cand []scored
	for _, p := range m.records {
		if filter.CatalogVersion != "" && p.CatalogVersion != filter.CatalogVersion {
			continue
		}
		if useAllow {
			if _, ok := allow[p.ToolName]; !ok {
				continue
			}
		}
		s := cosineSim(vector, p.Vector)
		cand = append(cand, scored{
			res: VectorSearchHit{
				ToolName:   p.ToolName,
				UpstreamID: p.UpstreamID,
				Score:      s,
			},
			score: s,
		})
	}
	sort.Slice(cand, func(i, j int) bool { return cand[i].score > cand[j].score })
	if len(cand) > topK {
		cand = cand[:topK]
	}
	out := make([]VectorSearchHit, len(cand))
	for i := range cand {
		out[i] = cand[i].res
	}
	return out, nil
}

func cosineSim(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i] * b[i])
	}
	if dot > cosineSimilarityMax {
		dot = cosineSimilarityMax
	}
	if dot < cosineSimilarityMin {
		dot = cosineSimilarityMin
	}
	return dot
}

func L2Normalize(v []float32) bool {
	var s float64
	for _, x := range v {
		s += float64(x * x)
	}
	n := math.Sqrt(s)
	if n == 0 {
		return false
	}
	inv := float32(1 / n)
	for i := range v {
		v[i] *= inv
	}
	return true
}
