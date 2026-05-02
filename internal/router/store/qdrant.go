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

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
)

// QdrantVectorStore talks to a Qdrant HTTP API for production-scale semantic index storage.
type QdrantVectorStore struct {
	baseURL    string
	collection string
	dim        int
	client     *http.Client
}

func NewQdrantVectorStore(baseURL, collection string, vectorDim int) (*QdrantVectorStore, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || collection == "" {
		return nil, fmt.Errorf("router/store/qdrant: baseURL and collection required")
	}
	if vectorDim <= 0 {
		return nil, fmt.Errorf("router/store/qdrant: vectorDim must be positive")
	}
	return &QdrantVectorStore{
		baseURL:    baseURL,
		collection: collection,
		dim:        vectorDim,
		client:     &http.Client{Timeout: defaults.QdrantHTTPClientTimeout},
	}, nil
}

func pointID(key string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}

func (q *QdrantVectorStore) doJSON(ctx context.Context, method, path string, body any, out any) (int, error) {
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
	raw, err := io.ReadAll(io.LimitReader(res.Body, defaults.MaxQdrantHTTPBodyBytes))
	if err != nil {
		return res.StatusCode, err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			snippet := len(raw)
			if snippet > defaults.MaxQdrantErrorSnippetBytes {
				snippet = defaults.MaxQdrantErrorSnippetBytes
			}
			return res.StatusCode, fmt.Errorf("router/store/qdrant: decode %s: %w (body=%s)", path, err, string(raw[:snippet]))
		}
	}
	return res.StatusCode, nil
}

func (q *QdrantVectorStore) Upsert(ctx context.Context, records []ToolVectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := q.validateRecordDims(records); err != nil {
		return err
	}
	if err := q.recreateCollection(ctx); err != nil {
		return err
	}
	return q.upsertPointsBatched(ctx, records)
}

func (q *QdrantVectorStore) validateRecordDims(records []ToolVectorRecord) error {
	for _, p := range records {
		if len(p.Vector) != q.dim {
			return ErrDimensionMismatch
		}
	}
	return nil
}

func (q *QdrantVectorStore) recreateCollection(ctx context.Context) error {
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
	return nil
}

func (q *QdrantVectorStore) upsertPointsBatched(ctx context.Context, records []ToolVectorRecord) error {
	batch := defaults.ReindexEmbedBatchSize
	for i := 0; i < len(records); i += batch {
		j := i + batch
		if j > len(records) {
			j = len(records)
		}
		chunk := records[i:j]
		if err := q.putPointsChunk(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (q *QdrantVectorStore) putPointsChunk(ctx context.Context, chunk []ToolVectorRecord) error {
	payloadPoints := make([]map[string]any, 0, len(chunk))
	for _, p := range chunk {
		payloadPoints = append(payloadPoints, map[string]any{
			"id":     pointID(p.ID),
			"vector": p.Vector,
			"payload": map[string]string{
				"tool_name": p.ToolName,
				"backend":   p.UpstreamID,
				"version":   p.CatalogVersion,
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
	return nil
}

func (q *QdrantVectorStore) DeleteCatalogVersion(ctx context.Context, version string) error {
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

func (q *QdrantVectorStore) Query(ctx context.Context, vector []float32, topK int, filter VectorSearchFilter) ([]VectorSearchHit, error) {
	if len(vector) != q.dim {
		return nil, ErrDimensionMismatch
	}
	if topK <= 0 {
		topK = defaults.DefaultVectorSearchTopK
	}

	var must []map[string]any
	if filter.CatalogVersion != "" {
		must = append(must, map[string]any{
			"key": "version", "match": map[string]any{"value": filter.CatalogVersion},
		})
	}
	if len(filter.AllowedToolNames) > 0 {
		must = append(must, map[string]any{
			"key": "tool_name", "match": map[string]any{"any": filter.AllowedToolNames},
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

	out := make([]VectorSearchHit, 0, len(resp.Result))
	for _, hit := range resp.Result {
		var meta map[string]any
		_ = json.Unmarshal(hit.Payload, &meta)
		out = append(out, VectorSearchHit{
			ToolName:   payloadString(meta["tool_name"]),
			UpstreamID: payloadString(meta["backend"]),
			Score:      hit.Score,
		})
	}
	return out, nil
}

func PingCollections(ctx context.Context, baseURL string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("router/store/qdrant: empty baseURL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/collections", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, defaults.MaxQdrantPingDiscardBytes))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("router/store/qdrant: ping: status %d", res.StatusCode)
	}
	return nil
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
