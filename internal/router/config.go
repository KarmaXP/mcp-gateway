package router

import "time"

// Config holds router tuning parameters (conservative defaults).
type Config struct {
	Mode Mode

	// TopK is the number of nearest neighbours requested from the vector store (before thresholding).
	TopK int
	// ScoreMin is the minimum cosine similarity [0,1] to accept a unique top-1 match.
	ScoreMin float64

	// HybridAlpha blends BM25 lexical scores with cosine: (1-α)*cosine + α*normBM25 on store results.
	// Zero disables hybrid reranking.
	HybridAlpha float64

	// AllowAutoRename permits replacing the host tool name with the vector winner when they differ.
	AllowAutoRename bool

	// EmbedTimeout bounds HTTP calls to the embedding service.
	EmbedTimeout time.Duration
	// QueryTimeout bounds store queries.
	QueryTimeout time.Duration
}

// DefaultConfig returns conservative defaults; routing stays off until Mode is set to something other than ModeOff.
func DefaultConfig() Config {
	return Config{
		Mode:            ModeOff,
		TopK:            8,
		ScoreMin:        0.35,
		HybridAlpha:     0,
		AllowAutoRename: false,
		EmbedTimeout:    10 * time.Second,
		QueryTimeout:    5 * time.Second,
	}
}
