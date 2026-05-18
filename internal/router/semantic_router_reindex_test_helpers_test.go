package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func reindexAndApply(t *testing.T, sr *SemanticRouter, ctx context.Context, version string, tools []IndexedTool) {
	t.Helper()
	require.NoError(t, sr.Reindex(ctx, version, tools))
	sr.ApplyCatalog(ctx, version, tools)
}
