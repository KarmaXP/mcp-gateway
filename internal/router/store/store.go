package store

import (
	"context"
)

type ToolVectorRecord struct {
	ID             string
	Vector         []float32
	ToolName       string
	UpstreamID     string
	CatalogVersion string
}

type VectorSearchFilter struct {
	CatalogVersion   string
	AllowedToolNames []string
}

type VectorSearchHit struct {
	ToolName   string
	UpstreamID string
	Score      float64
}

// Semantic router vector index (in-memory, Qdrant, etc.).
type Store interface {
	Upsert(ctx context.Context, records []ToolVectorRecord) error
	Query(ctx context.Context, vector []float32, topK int, filter VectorSearchFilter) ([]VectorSearchHit, error)
	DeleteCatalogVersion(ctx context.Context, version string) error
}
