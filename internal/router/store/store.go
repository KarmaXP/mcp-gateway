package store

import (
	"context"
)

// ToolVectorRecord is one tool embedding row for the semantic index (in-memory or Qdrant).
type ToolVectorRecord struct {
	ID             string
	Vector         []float32
	ToolName       string
	UpstreamID     string
	CatalogVersion string
}

// VectorSearchFilter scopes vector search by catalog version and optional allow-list of namespaced tool names.
type VectorSearchFilter struct {
	CatalogVersion   string
	AllowedToolNames []string
}

// VectorSearchHit is one similarity-ranked tool from the vector store.
type VectorSearchHit struct {
	ToolName   string
	UpstreamID string
	Score      float64
}

// Store persists and queries tool vectors for the semantic router.
type Store interface {
	Upsert(ctx context.Context, records []ToolVectorRecord) error
	Query(ctx context.Context, vector []float32, topK int, filter VectorSearchFilter) ([]VectorSearchHit, error)
	DeleteCatalogVersion(ctx context.Context, version string) error
}
