// Package store abstracts the vector index (Qdrant in production; in-memory fake in unit tests).
package store

import (
	"context"
)

// Point is one indexed tool vector with filterable payload (plan §3.B — Qdrant payload fields).
type Point struct {
	ID       string
	Vector   []float32
	ToolName string
	Backend  string
	Version  string
}

// Filter restricts search to a catalog version and optional allow-list (S1: enforced inside Query, not only post-filter).
type Filter struct {
	CatalogVersion string
	AllowedTools   []string // if non-empty, only these namespaced tools are considered
}

// Store is the minimal vector index contract (plan §3.B vector DB contract).
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
