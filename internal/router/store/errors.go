package store

import "errors"

// ErrDimensionMismatch is returned when a point vector length does not match the store dimension.
var ErrDimensionMismatch = errors.New("router/store: vector dimension mismatch")
