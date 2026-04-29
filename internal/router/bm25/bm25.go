package bm25

import (
	"math"
	"strings"
	"unicode"
)

const (
	k1 = 1.2
	b  = 0.75
)

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var cur []rune
	var out []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		out = append(out, string(cur))
		cur = cur[:0]
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func termFreq(doc string) map[string]int {
	toks := tokenize(doc)
	m := make(map[string]int, len(toks))
	for _, t := range toks {
		m[t]++
	}
	return m
}

func Score(query, doc string, avgDL float64, N int, df map[string]int) float64 {
	q := tokenize(query)
	if len(q) == 0 {
		return 0
	}
	tf := termFreq(doc)
	dl := float64(len(tokenize(doc)))
	if dl == 0 {
		dl = 1
	}
	if avgDL <= 0 {
		avgDL = dl
	}
	var sum float64
	for _, term := range q {
		f := float64(tf[term])
		if f == 0 {
			continue
		}
		dfi := df[term]
		if dfi == 0 {
			dfi = 1
		}
		idf := math.Log(1 + (float64(N)-float64(dfi)+0.5)/(float64(dfi)+0.5))
		denom := f + k1*(1-b+b*(dl/avgDL))
		sum += idf * (f * (k1 + 1)) / denom
	}
	return sum
}

func CorpusStats(docs []string) (avgDL float64, N int, df map[string]int) {
	N = len(docs)
	if N == 0 {
		return 1, 0, map[string]int{}
	}
	df = make(map[string]int)
	var totalLen float64
	for _, d := range docs {
		toks := tokenize(d)
		totalLen += float64(len(toks))
		seen := make(map[string]struct{}, len(toks))
		for _, t := range toks {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}
	return totalLen / float64(N), N, df
}

func RerankWeights(query string, docTexts []string, vectorScores []float64, alpha float64) []float64 {
	if alpha <= 0 {
		return append([]float64(nil), vectorScores...)
	}
	if len(docTexts) != len(vectorScores) {
		return append([]float64(nil), vectorScores...)
	}
	avgDL, N, df := CorpusStats(docTexts)
	raw := make([]float64, len(docTexts))
	for i, d := range docTexts {
		raw[i] = Score(query, d, avgDL, N, df)
	}
	maxR := 0.0
	for _, v := range raw {
		if v > maxR {
			maxR = v
		}
	}
	if maxR == 0 {
		maxR = 1
	}
	out := make([]float64, len(vectorScores))
	for i := range vectorScores {
		nrm := raw[i] / maxR
		if nrm > 1 {
			nrm = 1
		}
		out[i] = (1-alpha)*vectorScores[i] + alpha*nrm
	}
	return out
}
