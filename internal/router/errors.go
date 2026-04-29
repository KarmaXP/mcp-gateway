package router

import "errors"

var (
	errNilDeps = errors.New("missing dependencies")

	ErrStaleCatalog     = errors.New("router: stale catalog")
	ErrNoCandidates     = errors.New("router: no routing candidates")
	ErrBelowThreshold   = errors.New("router: score below minimum")
	ErrAmbiguous        = errors.New("router: ambiguous routing")
	ErrRenameDisallowed = errors.New("router: auto-rename disabled")
	ErrDegradedNoExact  = errors.New("router: embed failed and no exact match")
	ErrInvalidEmbedding = errors.New("router: invalid query embedding")
)
