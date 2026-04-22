package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientEmbedEmptyVectors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[],"dimensions":384}`))
	}))
	defer ts.Close()
	c := NewClient(ts.URL)
	_, err := c.Embed(context.Background(), []string{"hello"})
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "expected")
}

func TestClientEmbedHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overload", http.StatusServiceUnavailable)
	}))
	defer ts.Close()
	c := NewClient(ts.URL)
	_, err := c.Embed(context.Background(), []string{"a"})
	require.Error(t, err)
}

func TestClientEmbedOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2]],"dimensions":2}`))
	}))
	defer ts.Close()
	c := NewClient(ts.URL)
	out, err := c.Embed(context.Background(), []string{"one"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0], 2)
}
