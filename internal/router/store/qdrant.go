package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"
	"time"
)

// Qdrant is a minimal REST client for Qdrant vector search (Cosine, fixed dimension).
type Qdrant struct {
	baseURL    string
	collection string
	dim        int
	client     *http.Client
}

// NewQdrant builds a store targeting baseURL (e.g. http://127.0.0.1:6333) and collection name.
func NewQdrant(baseURL, collection string, vectorDim int) (*Qdrant, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || collection == "" {
		return nil, fmt.Errorf("router/store/qdrant: baseURL and collection required")
	}
	if vectorDim <= 0 {
		return nil, fmt.Errorf("router/store/qdrant: vectorDim must be positive")
	}
	return &Qdrant{
		baseURL:    baseURL,
		collection: collection,
		dim:        vectorDim,
		client:     &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func pointID(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

func (q *Qdrant) doJSON(ctx context.Context, method, path string, body any, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := q.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return res.StatusCode, err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			snippet := len(raw)
			if snippet > 200 {
				snippet = 200
			}
			return res.StatusCode, fmt.Errorf("router/store/qdrant: decode %s: %w (body=%s)", path, err, string(raw[:snippet]))
		}
	}
	return res.StatusCode, nil
}

// Upsert replaces the collection contents (same semantics as Memory: one catalog snapshot).
func (q *Qdrant) Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	for _, p := range points {
		if len(p.Vector) != q.dim {
			return ErrDimensionMismatch
		}
	}

	// Drop and recreate collection so the snapshot matches in-memory replace semantics.
	delStatus, _ := q.doJSON(ctx, http.MethodDelete, "/collections/"+q.collection, nil, nil)
	if delStatus != http.StatusOK && delStatus != http.StatusNotFound {
		return fmt.Errorf("router/store/qdrant: delete collection: status %d", delStatus)
	}
	createBody := map[string]any{
		"vectors": map[string]any{
			"size":     q.dim,
			"distance": "Cosine",
		},
	}
	st, err := q.doJSON(ctx, http.MethodPut, "/collections/"+q.collection, createBody, nil)
	if err != nil {
		return err
	}
	if st != http.StatusOK {
		return fmt.Errorf("router/store/qdrant: create collection: status %d", st)
	}

	const batch = 64
	for i := 0; i < len(points); i += batch {
		j := i + batch
		if j > len(points) {
			j = len(points)
		}
		chunk := points[i:j]
		payloadPoints := make([]map[string]any, 0, len(chunk))
		for _, p := range chunk {
			payloadPoints = append(payloadPoints, map[string]any{
				"id":     pointID(p.ID),
				"vector": p.Vector,
				"payload": map[string]string{
					"tool_name": p.ToolName,
					"backend":   p.Backend,
					"version":   p.Version,
				},
			})
		}
		st, err := q.doJSON(ctx, http.MethodPut, "/collections/"+q.collection+"/points?wait=true", map[string]any{
			"points": payloadPoints,
		}, nil)
		if err != nil {
			return err
		}
		if st != http.StatusOK {
			return fmt.Errorf("router/store/qdrant: upsert points: status %d", st)
		}
	}
	return nil
}

// DeleteCatalogVersion removes points tagged with the given catalog version in payload.version.
func (q *Qdrant) DeleteCatalogVersion(ctx context.Context, version string) error {
	if version == "" {
		return nil
	}
	body := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "version", "match": map[string]any{"value": version}},
			},
		},
	}
	st, err := q.doJSON(ctx, http.MethodPost, "/collections/"+q.collection+"/points/delete?wait=true", body, nil)
	if err != nil {
		return err
	}
	if st != http.StatusOK {
		return fmt.Errorf("router/store/qdrant: delete by version: status %d", st)
	}
	return nil
}

// Query runs cosine similarity search with mandatory filters (catalog + optional allow-list).
func (q *Qdrant) Query(ctx context.Context, vector []float32, topK int, filter Filter) ([]Result, error) {
	if len(vector) != q.dim {
		return nil, ErrDimensionMismatch
	}
	if topK <= 0 {
		topK = 8
	}

	var must []map[string]any
	if filter.CatalogVersion != "" {
		must = append(must, map[string]any{
			"key": "version", "match": map[string]any{"value": filter.CatalogVersion},
		})
	}
	if len(filter.AllowedTools) > 0 {
		must = append(must, map[string]any{
			"key": "tool_name", "match": map[string]any{"any": filter.AllowedTools},
		})
	}

	reqBody := map[string]any{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
	}
	if len(must) > 0 {
		reqBody["filter"] = map[string]any{"must": must}
	}

	var resp struct {
		Result []struct {
			ID      any             `json:"id"`
			Score   float64         `json:"score"`
			Payload json.RawMessage `json:"payload"`
		} `json:"result"`
	}
	st, err := q.doJSON(ctx, http.MethodPost, "/collections/"+q.collection+"/points/search", reqBody, &resp)
	if err != nil {
		return nil, err
	}
	if st != http.StatusOK {
		return nil, fmt.Errorf("router/store/qdrant: search: status %d", st)
	}

	out := make([]Result, 0, len(resp.Result))
	for _, hit := range resp.Result {
		var meta map[string]any
		_ = json.Unmarshal(hit.Payload, &meta)
		out = append(out, Result{
			ToolName: payloadString(meta["tool_name"]),
			Backend:  payloadString(meta["backend"]),
			Score:    hit.Score,
		})
	}
	return out, nil
}

func payloadString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
