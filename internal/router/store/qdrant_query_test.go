package store

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQdrantVectorStoreQueryRequiresCatalogVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	q, err := NewQdrantVectorStore(srv.URL, "c", 2)
	require.NoError(t, err)
	out, err := q.Query(context.Background(), []float32{1, 0}, 4, VectorSearchFilter{})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestQdrantVectorStoreQueryEmptyAllowedBlocksAll(t *testing.T) {
	ctx := context.Background()
	searchCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/points/search") {
			searchCalls++
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	q, err := NewQdrantVectorStore(srv.URL, "c", 2)
	require.NoError(t, err)
	out, err := q.Query(ctx, []float32{1, 0}, 4, VectorSearchFilter{
		CatalogVersion:   "v1",
		AllowedToolNames: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, out)
	require.Zero(t, searchCalls)
}

func TestQdrantVectorStoreUpsertUsesUUIDPointID(t *testing.T) {
	ctx := context.Background()
	var upsertBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/collections/tcol"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/collections/tcol"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && strings.Contains(path, "/collections/tcol/points"):
			b, _ := io.ReadAll(r.Body)
			upsertBody = string(b)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	q, err := NewQdrantVectorStore(srv.URL, "tcol", 2)
	require.NoError(t, err)
	key := "v1::a__x"
	require.NoError(t, q.Upsert(ctx, []ToolVectorRecord{
		{ID: key, Vector: []float32{1, 0}, ToolName: "a__x", UpstreamID: "b1", CatalogVersion: "v1"},
	}))
	require.Contains(t, upsertBody, pointID(key))
	require.NotRegexp(t, `"id":\s*\d+`, upsertBody)
}
