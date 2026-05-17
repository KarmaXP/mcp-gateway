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

func TestQdrantVectorStoreUpsertQueryDeleteAgainstMockAPI(t *testing.T) {
	ctx := context.Background()
	var searchBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/collections/tcol"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/collections/tcol"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && strings.Contains(path, "/collections/tcol/points"):
			b, _ := io.ReadAll(r.Body)
			_ = b
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.Contains(path, "/points/search"):
			b, _ := io.ReadAll(r.Body)
			searchBody = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"id":1,"score":0.91,"payload":{"tool_name":"a__x","backend":"b1","version":"v1"}}],"status":"ok"}`))
		case r.Method == http.MethodPost && strings.Contains(path, "/points/delete"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	q, err := NewQdrantVectorStore(srv.URL, "tcol", 2)
	require.NoError(t, err)

	require.NoError(t, q.Upsert(ctx, []ToolVectorRecord{
		{ID: "v1::a__x", Vector: []float32{1, 0}, ToolName: "a__x", UpstreamID: "b1", CatalogVersion: "v1"},
	}))

	res, err := q.Query(ctx, []float32{1, 0}, 4, VectorSearchFilter{CatalogVersion: "v1"})
	require.NoError(t, err)
	require.Contains(t, searchBody, "v1")
	require.Len(t, res, 1)
	require.Equal(t, "a__x", res[0].ToolName)
	require.Equal(t, "b1", res[0].UpstreamID)
	require.InDelta(t, 0.91, res[0].Score, 1e-6)

	require.NoError(t, q.DeleteCatalogVersion(ctx, "v1"))
}

func TestQdrantVectorStoreSecondUpsertDoesNotDeleteCollection(t *testing.T) {
	ctx := context.Background()
	var deleteCollection int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodDelete && strings.Contains(path, "/collections/tcol"):
			deleteCollection++
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/collections/tcol"):
			if deleteCollection == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/collections/tcol"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && strings.Contains(path, "/collections/tcol/points"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	q, err := NewQdrantVectorStore(srv.URL, "tcol", 2)
	require.NoError(t, err)
	rec := ToolVectorRecord{ID: "v1::a__x", Vector: []float32{1, 0}, ToolName: "a__x", UpstreamID: "b1", CatalogVersion: "v1"}
	require.NoError(t, q.Upsert(ctx, []ToolVectorRecord{rec}))
	require.NoError(t, q.Upsert(ctx, []ToolVectorRecord{rec}))
	require.Equal(t, 0, deleteCollection, "incremental upsert must not delete the collection")
}

func TestQdrantVectorStoreQueryDimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	q, err := NewQdrantVectorStore(srv.URL, "c", 3)
	require.NoError(t, err)
	_, err = q.Query(context.Background(), []float32{1, 1}, 2, VectorSearchFilter{})
	require.ErrorIs(t, err, ErrDimensionMismatch)
}

func TestPingCollections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/collections" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	require.NoError(t, PingCollections(context.Background(), srv.URL))
}
