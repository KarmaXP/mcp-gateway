//go:build integration

package router

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

func TestSemanticVectorRoutingWithQdrantAndMiniLM(t *testing.T) {
	ctx := context.Background()
	qURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qURL == "" {
		qURL = "http://127.0.0.1:6333"
	}
	embURL := strings.TrimSpace(os.Getenv("EMBED_URL"))
	if embURL == "" {
		embURL = "http://127.0.0.1:18001"
	}

	if ok := probeURL(ctx, qURL+"/collections"); !ok {
		t.Skip("Qdrant not reachable at ", qURL, " — start compose (qdrant service)")
	}
	if ok := probeURL(ctx, embURL+"/healthz"); !ok {
		t.Skip("embed service not reachable at ", embURL)
	}

	coll := "mcp_router_itest_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	st, err := store.NewQdrant(qURL, coll, 384)
	require.NoError(t, err)
	t.Cleanup(func() { _ = httpDelete(ctx, qURL+"/collections/"+coll) })

	cfg := DefaultConfig()
	cfg.Mode = ModeAssistList
	cfg.ScoreMin = 0.22
	cfg.TopK = 8
	cfg.AllowAutoRename = true
	cfg.EmbedTimeout = 60 * time.Second
	cfg.QueryTimeout = 30 * time.Second

	eng := NewEngine(cfg, embed.NewClient(embURL), st, 384)

	listJSON := []byte(`{"tools":[
		{"name":"alpha__echo","description":"mock tool echo","inputSchema":{"type":"object","properties":{}}},
		{"name":"beta__ping","description":"mock tool ping","inputSchema":{"type":"object","properties":{}}}
	]}`)
	entries, err := BuildCatalogEntries(listJSON, func(prefix string) (string, error) {
		switch prefix {
		case "alpha":
			return "backend-alpha", nil
		case "beta":
			return "backend-beta", nil
		default:
			return "", fmt.Errorf("unknown prefix %q", prefix)
		}
	})
	require.NoError(t, err)

	ver := "itest-" + uuid.NewString()
	require.NoError(t, eng.Reindex(ctx, ver, entries))

	tool, dec, err := eng.ResolveToolsCall(ctx, RoutingSignal{
		ToolName:   "repeat user text back to them",
		IntentText: "the user wants an echo style response",
	})
	require.NoError(t, err)
	require.Equal(t, "alpha__echo", tool)
	require.Equal(t, OutcomeVectorHit, dec.Outcome)
	require.Equal(t, "backend-alpha", dec.BackendID)
}

func probeURL(ctx context.Context, u string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode < 500
}

func httpDelete(ctx context.Context, u string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	return nil
}
