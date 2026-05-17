//go:build integration

package eval

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/embed"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

const (
	minilmProbeTimeout = 2 * time.Second
	minilmEmbedTimeout = 60 * time.Second
	minilmQueryTimeout = 30 * time.Second
	minilmQdrantCollection = "mcp_router_eval_itest_"
	minilmRecall1Threshold = 0.60
	minilmRecall3Threshold = 0.85
)

// go test -tags=integration -race ./internal/router/eval -run TestRouterEvalVectorRecallMiniLM -v
func TestRouterEvalVectorRecallMiniLM(t *testing.T) {
	ctx := context.Background()
	qURL := defaultFromEnv("QDRANT_URL", defaults.DefaultQdrantHTTPURL)
	embURL := defaultFromEnv("EMBED_URL", defaults.DefaultEmbedServiceURL)

	skipUnlessIntegrationDeps(t, probeURL(ctx, qURL+"/collections"),
		"Qdrant not reachable at %s — start compose (qdrant service)", qURL)
	skipUnlessIntegrationDeps(t, probeURL(ctx, embURL+"/healthz"),
		"embed service not reachable at %s (compose maps embed to host port 8001 by default)", embURL)

	catalog, source := routerEvalCatalogForIntegration(t)
	require.GreaterOrEqual(t, len(catalog), 20, "router eval harness expects >=20 tools")
	cases := goldenCasesFromCatalog(catalog)
	require.NotEmpty(t, cases, "need at least one labeled intent case")

	coll := minilmQdrantCollection + strings.ReplaceAll(uuid.NewString(), "-", "_")
	st, err := store.NewQdrantVectorStore(qURL, coll, defaults.VectorDimension)
	require.NoError(t, err)
	t.Cleanup(func() { _ = httpDelete(ctx, qURL+"/collections/"+coll) })

	cfg := router.DefaultSemanticRouterRuntimeConfig()
	cfg.Mode = router.ModeAssistList
	cfg.TopK = defaults.RouterTopK
	cfg.ScoreMin = defaults.RouterScoreMin
	cfg.HybridAlpha = 0.2
	cfg.AllowAutoRename = true
	cfg.EmbedTimeout = minilmEmbedTimeout
	cfg.QueryTimeout = minilmQueryTimeout
	sr := router.NewSemanticRouter(cfg, embed.NewClient(embURL), st, defaults.VectorDimension)

	ver := "router-eval-itest-" + uuid.NewString()
	require.NoError(t, sr.Reindex(ctx, ver, catalog))
	require.Equal(t, ver, sr.CatalogVersion())

	hitsAt1 := 0
	hitsAt3 := 0
	for _, tc := range cases {
		got, dec, err := sr.ResolveToolsCall(ctx, router.RoutingSignal{
			ToolName:       "wrong__tool_name",
			IntentText:     tc.Intent,
			CatalogVersion: ver,
			AllowedTools:   tc.Allowed,
		})
		if err != nil {
			if errors.Is(err, router.ErrAmbiguous) {
				if candidateInTopK(dec.Candidates, tc.WantTool, 3) {
					hitsAt3++
				}
				t.Logf("ambiguous routing (recall@1 miss): intent=%q want=%s candidates=%v", tc.Intent, tc.WantTool, dec.Candidates)
				continue
			}
			require.NoError(t, err, "intent=%q want=%s", tc.Intent, tc.WantTool)
		}
		if got == tc.WantTool {
			hitsAt1++
		}
		if candidateInTopK(dec.Candidates, tc.WantTool, 3) {
			hitsAt3++
		}
	}

	total := len(cases)
	recallAt1 := float64(hitsAt1) / float64(total)
	recallAt3 := float64(hitsAt3) / float64(total)
	t.Logf("MiniLM+Qdrant router eval (%s): recall@1=%.3f (%d/%d) recall@3=%.3f (%d/%d)",
		source, recallAt1, hitsAt1, total, recallAt3, hitsAt3, total)

	require.GreaterOrEqual(t, recallAt1, minilmRecall1Threshold, "recall@1 regression")
	require.GreaterOrEqual(t, recallAt3, minilmRecall3Threshold, "recall@3 regression")
}

type recallCase struct {
	Intent   string
	WantTool string
	Allowed  []string
}

func goldenCasesFromCatalog(cat []router.IndexedTool) []recallCase {
	cases := make([]recallCase, 0, len(cat))
	for _, tool := range cat {
		intent := strings.TrimSpace(tool.ToolRow.Description)
		if intent == "" {
			intent = tool.ToolRow.Name
		}
		cases = append(cases, recallCase{
			Intent:   intent,
			WantTool: tool.ToolRow.Name,
		})
	}
	return cases
}

func routerEvalCatalogForIntegration(t *testing.T) ([]router.IndexedTool, string) {
	t.Helper()

	candidates := []string{
		strings.TrimSpace(os.Getenv("ROUTER_EVAL_CATALOG_PATH")),
		"../../../docs/evaluation/router-eval-catalog.json",
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.Getenv("ROUTER_EVAL_CATALOG_PATH") == path {
				t.Fatalf("read ROUTER_EVAL_CATALOG_PATH %q: %v", path, err)
			}
			continue
		}
		catalog, err := router.BuildIndexedTools(raw, func(prefix string) (string, error) {
			return "b_" + prefix, nil
		})
		if err != nil {
			if os.Getenv("ROUTER_EVAL_CATALOG_PATH") == path {
				t.Fatalf("parse ROUTER_EVAL_CATALOG_PATH %q: %v", path, err)
			}
			t.Logf("skipping unreadable router eval catalog %s: %v", path, err)
			continue
		}
		if len(catalog) > 0 {
			return catalog, path
		}
	}

	return SyntheticCatalog(), "SyntheticCatalog() fallback"
}

func defaultFromEnv(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func candidateInTopK(cands []router.ScoredTool, want string, topK int) bool {
	limit := topK
	if limit > len(cands) {
		limit = len(cands)
	}
	for i := 0; i < limit; i++ {
		if cands[i].Name == want {
			return true
		}
	}
	return false
}

func probeURL(ctx context.Context, u string) bool {
	ctx, cancel := context.WithTimeout(ctx, minilmProbeTimeout)
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
