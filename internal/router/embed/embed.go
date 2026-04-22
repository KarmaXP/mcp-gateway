// Package embed is an HTTP client for the ONNX embedding sidecar (POST /embed).
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Embedder produces L2-normalised 384-d vectors (all-MiniLM-L6-v2).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Client calls the FastAPI service in deployments/embed/server.py.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a client; baseURL should be like "http://127.0.0.1:8001" (no trailing slash).
// The HTTP transport pools idle connections for reuse across embedding calls.
func NewClient(baseURL string) *Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   60 * time.Second,
			Transport: tr,
		},
	}
}

// WithHTTPClient overrides the HTTP client (e.g. shorter timeouts in tests).
func (c *Client) WithHTTPClient(h *http.Client) *Client {
	c.httpClient = h
	return c
}

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Dimensions int         `json:"dimensions"`
}

// Embed implements Embedder.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("embed: empty texts")
	}
	body, err := json.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: http: %w", err)
	}
	defer res.Body.Close()
	rb, err := io.ReadAll(io.LimitReader(res.Body, 1<<22))
	if err != nil {
		return nil, fmt.Errorf("embed: read body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: HTTP %d: %s", res.StatusCode, string(bytes.TrimSpace(rb)))
	}
	var out embedResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d vectors, got %d", len(texts), len(out.Embeddings))
	}
	vecs := make([][]float32, len(out.Embeddings))
	for i, row := range out.Embeddings {
		vecs[i] = make([]float32, len(row))
		for j, v := range row {
			vecs[i][j] = float32(v)
		}
	}
	return vecs, nil
}
