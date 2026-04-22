package router

import "time"

// Config holds router hyperparameters (plan §3.B.5 / §4.5 — defaults conservative).
type Config struct {
	Mode Mode

	// TopK is the number of nearest neighbours requested from the vector store (before thresholding).
	TopK int
	// ScoreMin is the minimum cosine similarity [0,1] to accept a unique top-1 match.
	ScoreMin float64

	// AllowAutoRename permits replacing the host tool name with the vector winner when they differ.
	AllowAutoRename bool

	// EmbedTimeout bounds HTTP calls to the embedding service (R5).
	EmbedTimeout time.Duration
	// QueryTimeout bounds store queries.
	QueryTimeout time.Duration
}

// DefaultConfig returns thesis-friendly defaults; ModeOff must be set explicitly to enable routing.
func DefaultConfig() Config {
	return Config{
		Mode:            ModeOff,
		TopK:            8,
		ScoreMin:        0.35,
		AllowAutoRename: false,
		EmbedTimeout:    10 * time.Second,
		QueryTimeout:    5 * time.Second,
	}
}
