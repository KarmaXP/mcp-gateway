package bm25

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRerankWeights_prefersLexicalWhenAlphaHigh(t *testing.T) {
	docs := []string{
		"tool alpha widget catalog",
		"tool beta unrelated text",
	}
	vec := []float64{0.9, 0.91} // vector slightly prefers wrong doc
	q := "widget catalog lookup"
	w := RerankWeights(q, docs, vec, 0.5)
	require.Greater(t, w[0], w[1])
}
