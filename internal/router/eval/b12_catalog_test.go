package eval

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/router"
)

func TestB12CatalogJSONLoads(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	catalogPath := filepath.Join(repoRoot, "docs", "evaluation", "b1.2-catalog.json")
	raw, err := os.ReadFile(catalogPath)
	require.NoError(t, err, "docs/evaluation/b1.2-catalog.json must exist (make gen-b12-catalog)")

	catalog, err := router.BuildIndexedTools(raw, func(prefix string) (string, error) {
		return "b_" + prefix, nil
	})
	require.NoError(t, err)
	require.Equal(t, len(SyntheticCatalog()), len(catalog))

	names := make(map[string]struct{}, len(catalog))
	for _, tool := range catalog {
		names[tool.ToolRow.Name] = struct{}{}
	}
	for _, want := range []string{"k8s__get_pod_logs", "prom__query_instant", "gh__list_prs"} {
		_, ok := names[want]
		require.True(t, ok, "catalog missing %s", want)
	}
}
