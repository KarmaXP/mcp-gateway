// Package store abstracts the vector index (Qdrant in production; in-memory fake in unit tests).
package store

import (
	"context"
)

// Point is one indexed tool vector plus Qdrant payload fields (tool name, backend, catalog version).
type Point struct {
	ID       string
	Vector   []float32
	ToolName string
	Backend  string
	Version  string
}

// Filter restricts search by catalog version and optional tool allow-list (applied inside Query).
type Filter struct {
	CatalogVersion string
	AllowedTools   []string // if non-empty, only these namespaced tools are considered
}

// Store is the vector index used by the semantic router.
type Store interface {
	Upsert(ctx context.Context, points []Point) error
	Query(ctx context.Context, vector []float32, topK int, filter Filter) ([]Result, error)
	DeleteCatalogVersion(ctx context.Context, version string) error
}

// Result is one scored neighbour.
type Result struct {
	ToolName string
	Backend  string
	Score    float64 // cosine similarity for L2-normalised vectors
}
