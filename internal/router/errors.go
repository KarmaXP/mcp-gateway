package router

import "errors"

// Sentinel errors for errors.Is and wrapping on routing miss paths.
var (
	errNilDeps = errors.New("missing dependencies")

	// ErrStaleCatalog means the client pinned a catalog version that does not match the server index.
	ErrStaleCatalog = errors.New("router: stale catalog")
	// ErrNoCandidates means the vector store returned no neighbours for the query.
	ErrNoCandidates = errors.New("router: no routing candidates")
	// ErrBelowThreshold means the top score was below ScoreMin.
	ErrBelowThreshold = errors.New("router: score below minimum")
	// ErrAmbiguous means two or more candidates tied above the acceptance threshold.
	ErrAmbiguous = errors.New("router: ambiguous routing")
	// ErrRenameDisallowed means vector resolution picked a different tool name but AllowAutoRename is false.
	ErrRenameDisallowed = errors.New("router: auto-rename disabled")
	// ErrDegradedNoExact means embedding failed and there was no exact catalog match to fall back to.
	ErrDegradedNoExact = errors.New("router: embed failed and no exact match")
	// ErrInvalidEmbedding means the embedder returned a wrong-shaped vector for the query.
	ErrInvalidEmbedding = errors.New("router: invalid query embedding")
)
