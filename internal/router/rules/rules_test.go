package rules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalAlias(t *testing.T) {
	r := New(map[string]string{"OLD__logs": "k8s__get_logs"}, nil)
	require.Equal(t, "k8s__get_logs", r.CanonicalAlias("old__logs"))
	require.Equal(t, "", r.CanonicalAlias("missing"))
}

func TestNarrowAllowed_withAllowList(t *testing.T) {
	r := New(nil, map[string]string{"kubernetes": "k8s"})
	catalog := []string{"k8s__get_logs", "aws__list_buckets", "k8s__list_pods"}
	allowed := []string{"k8s__get_logs", "aws__list_buckets"}

	out := r.NarrowAllowed("fetch kubernetes pod logs", allowed, catalog)
	require.ElementsMatch(t, []string{"k8s__get_logs"}, out)
}

func TestNarrowAllowed_emptyAllowUsesCatalog(t *testing.T) {
	r := New(nil, map[string]string{"aws": "aws"})
	catalog := []string{"k8s__get_logs", "aws__list_buckets"}
	out := r.NarrowAllowed("list aws buckets", nil, catalog)
	require.ElementsMatch(t, []string{"aws__list_buckets"}, out)
}

func TestNarrowAllowed_noKeywordNoop(t *testing.T) {
	r := New(nil, map[string]string{"aws": "aws"})
	out := r.NarrowAllowed("generic intent", []string{"a"}, []string{"a", "b"})
	require.Equal(t, []string{"a"}, out)
}
