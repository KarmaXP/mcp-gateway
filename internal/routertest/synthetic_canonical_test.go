package routertest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyntheticCatalogCanonicalSRETools(t *testing.T) {
	names := make(map[string]struct{}, len(SyntheticCatalog()))
	for _, entry := range SyntheticCatalog() {
		names[entry.ToolRow.Name] = struct{}{}
	}
	for _, want := range []string{
		"k8s__get_pod_logs",
		"prom__query_instant",
		"gh__list_prs",
	} {
		_, ok := names[want]
		require.True(t, ok, "SyntheticCatalog missing %s", want)
	}
}
