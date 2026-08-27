package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchToolPatternIsNotPathMatching(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tool    string
		pattern string
		want    bool
	}{
		{name: "a separator does not stop the star", tool: "a/b/c", pattern: "a*c", want: true},
		{name: "a backslash separator does not either", tool: "a\\b", pattern: "a*b", want: true},
		{name: "case is significant", tool: "K8S__get_pod", pattern: "k8s__*", want: false},
		{name: "question mark does not span two bytes", tool: "k8s__ab", pattern: "k8s__?", want: false},
		{name: "star matches an empty run", tool: "k8s__", pattern: "k8s__*", want: true},
		{name: "star in the middle", tool: "k8s__get_pod", pattern: "k8s*pod", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := matchTool(tc.tool, tc.pattern)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMatchToolRejectsUnsupportedPatternSyntax(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"k8s__[a-z]*", "k8s__x]", `k8s__\*`} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			_, err := matchTool("k8s__get_pod", pattern)
			require.Error(t, err, "character classes and escapes are path syntax, not tool syntax")
			require.ErrorContains(t, err, "use only * and ?")
		})
	}
}

func TestElevatedToolsHonourPatterns(t *testing.T) {
	t.Parallel()
	e := NewEngine(EngineInput{ElevatedTools: []string{"k8s__*"}})
	require.True(t, e.RequiresInputSchema("k8s__delete_pod"),
		"an elevated pattern must gate the strict schema requirement it was written for")
	require.False(t, e.RequiresInputSchema("gh__list_prs"))
}
