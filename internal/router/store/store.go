package store

import (
	"context"
)

type Point struct {
	ID       string
	Vector   []float32
	ToolName string
	Backend  string
	Version  string
}

type Filter struct {
	CatalogVersion string
	AllowedTools   []string
}

type Store interface {
	Upsert(ctx context.Context, points []Point) error
	Query(ctx context.Context, vector []float32, topK int, filter Filter) ([]Result, error)
	DeleteCatalogVersion(ctx context.Context, version string) error
}

type Result struct {
	ToolName string
	Backend  string
	Score    float64
}
