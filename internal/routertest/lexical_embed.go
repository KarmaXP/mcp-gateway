package routertest

import (
	"context"
	"hash/fnv"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

const lexicalEmbedUnitAxisValue float32 = 1

type LexicalEmbedder struct {
	Dim int
}

func tokenizeLex(s string) []string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func fnv32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func (l LexicalEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dim := l.Dim
	if dim <= 0 {
		dim = defaults.VectorDimension
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, dim)
		for _, w := range tokenizeLex(t) {
			v[int(fnv32(w))%dim] += 1
		}
		if !store.L2Normalize(v) {
			v[0] = lexicalEmbedUnitAxisValue
			_ = store.L2Normalize(v)
		}
		out[i] = v
	}
	return out, nil
}
