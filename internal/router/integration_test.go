//go:build integration

package router

import (
	"context"
	"fmt"
	"github.com/KarmaXP/mcp-gateway/internal/router/mode"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

const (
	integrationProbeTimeout = 2 * time.Second
	integrationScoreMin = 0.22
	integrationEmbedTimeout = 60 * time.Second
	integrationQueryTimeout = 30 * time.Second

	integrationQdrantCollectionPrefix = "mcp_router_itest_"
)

func TestSemanticVectorRoutingWithQdrantAndMiniLM(t *testing.T) {
	ctx := context.Background()
	qURL := strings.TrimSpace(os.Getenv("QDRANT_URL"))
	if qURL == "" {
		qURL = defaults.DefaultQdrantHTTPURL
	}
	embURL := strings.TrimSpace(os.Getenv("EMBED_URL"))
	if embURL == "" {
		embURL = defaults.DefaultEmbedServiceURL
	}

	skipUnlessIntegrationDeps(t, probeURL(ctx, qURL+"/collections"),
		"Qdrant not reachable at %s; start compose qdrant service", qURL)
	skipUnlessIntegrationDeps(t, probeEmbedService(ctx, embURL),
		"embed service not reachable at %s; start compose embed service", embURL)

	coll := integrationQdrantCollectionPrefix + strings.ReplaceAll(uuid.NewString(), "-", "_")
	st, err := store.NewQdrantVectorStore(qURL, coll, defaults.VectorDimension)
	require.NoError(t, err)
	t.Cleanup(func() { _ = httpDelete(ctx, qURL+"/collections/"+coll) })

	cfg := DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = mode.AssistList
	cfg.ScoreMin = integrationScoreMin
	cfg.TopK = defaults.RouterTopK
	cfg.AllowAutoRename = true
	cfg.EmbedTimeout = integrationEmbedTimeout
	cfg.QueryTimeout = integrationQueryTimeout

	sr := NewSemanticRouter(cfg, embed.NewClient(embURL), st, defaults.VectorDimension)

	listJSON := []byte(`{"tools":[
		{"name":"alpha__echo","description":"mock tool echo","inputSchema":{"type":"object","properties":{}}},
		{"name":"beta__ping","description":"mock tool ping","inputSchema":{"type":"object","properties":{}}}
	]}`)
	indexed, err := BuildIndexedTools(listJSON, func(prefix string) (string, error) {
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
	reindexAndApply(t, sr, ctx, ver, indexed)

	tool, dec, err := sr.ResolveToolsCall(ctx, RoutingSignal{
		ToolName:   "repeat user text back to them",
		IntentText: "the user wants an echo style response",
	})
	require.NoError(t, err)
	require.Equal(t, "alpha__echo", tool)
	require.Equal(t, OutcomeVectorHit, dec.Outcome)
	require.Equal(t, "backend-alpha", dec.UpstreamID)
}

func probeURL(ctx context.Context, u string) bool {
	ctx, cancel := context.WithTimeout(ctx, integrationProbeTimeout)
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
	return res.StatusCode < http.StatusInternalServerError
}

func probeEmbedService(ctx context.Context, embURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, integrationProbeTimeout)
	defer cancel()
	c := embed.NewClient(embURL)
	_, err := c.Embed(ctx, []string{"integration probe"})
	return err == nil
}

func skipUnlessIntegrationDeps(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if ok {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if os.Getenv("CI") != "" {
		t.Fatal(msg)
	}
	t.Skip(msg)
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
