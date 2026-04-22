package store

import (
	"context"
	"math"
	"sync"
)

// Memory is a thread-safe in-memory Store for tests and dev without Qdrant (dimension must match embeddings).
type Memory struct {
	dim int

	mu     sync.RWMutex
	points []Point
}

// NewMemory creates an empty store. All vectors must have length dim.
func NewMemory(dim int) *Memory {
	return &Memory{dim: dim}
}

// Upsert implements Store.
func (m *Memory) Upsert(ctx context.Context, points []Point) error {
	_ = ctx
	for _, p := range points {
		if len(p.Vector) != m.dim {
			return ErrDimensionMismatch
		}
	}
	m.mu.Lock()
	m.points = append([]Point(nil), points...)
	m.mu.Unlock()
	return nil
}

// DeleteCatalogVersion removes points for a version (full rebuild pattern).
func (m *Memory) DeleteCatalogVersion(ctx context.Context, version string) error {
	_ = ctx
	m.mu.Lock()
	keep := m.points[:0]
	for _, p := range m.points {
		if p.Version != version {
			keep = append(keep, p)
		}
	}
	m.points = keep
	m.mu.Unlock()
	return nil
}

// Query implements Store with cosine similarity; applies Filter before ranking (S1).
func (m *Memory) Query(ctx context.Context, vector []float32, topK int, filter Filter) ([]Result, error) {
	_ = ctx
	if len(vector) != m.dim {
		return nil, ErrDimensionMismatch
	}
	if topK <= 0 {
		topK = 8
	}
	allow := map[string]struct{}{}
	useAllow := len(filter.AllowedTools) > 0
	for _, n := range filter.AllowedTools {
		allow[n] = struct{}{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	type scored struct {
		res   Result
		score float64
	}
	var cand []scored
	for _, p := range m.points {
		if filter.CatalogVersion != "" && p.Version != filter.CatalogVersion {
			continue
		}
		if useAllow {
			if _, ok := allow[p.ToolName]; !ok {
				continue
			}
		}
		s := cosineSim(vector, p.Vector)
		cand = append(cand, scored{
			res: Result{
				ToolName: p.ToolName,
				Backend:  p.Backend,
				Score:    s,
			},
			score: s,
		})
	}
	// partial sort topK
	for i := 0; i < len(cand); i++ {
		for j := i + 1; j < len(cand); j++ {
			if cand[j].score > cand[i].score {
				cand[i], cand[j] = cand[j], cand[i]
			}
		}
	}
	if len(cand) > topK {
		cand = cand[:topK]
	}
	out := make([]Result, len(cand))
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
	// assume L2-normalised; clamp
	if dot > 1 {
		dot = 1
	}
	if dot < -1 {
		dot = -1
	}
	return dot
}

// L2 normalises v in place; returns false if norm is zero.
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
