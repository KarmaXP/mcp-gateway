package index

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatDocumentStable(t *testing.T) {
	a := FormatDocument(ToolRow{Name: "alpha__echo", Description: "d", ParamKeys: []string{"z", "a"}})
	b := FormatDocument(ToolRow{Name: "alpha__echo", Description: "d", ParamKeys: []string{"a", "z"}})
	require.Equal(t, a, b)
	require.Contains(t, a, "alpha__echo")
	require.Contains(t, a, "Template: v1")
}

func TestParseToolsListJSON(t *testing.T) {
	raw := []byte(`{"tools":[{"name":"p__t","description":"hi","inputSchema":{"type":"object","properties":{"x":{"type":"string"}}}}]}`)
	rows, err := ParseToolsListJSON(raw)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "p__t", rows[0].Name)
	require.Contains(t, rows[0].ParamKeys, "x")
}

func TestFormatQueryIncludesIntent(t *testing.T) {
	q := FormatQuery("foo", "do something", []string{"b", "a"})
	require.True(t, strings.Contains(q, "do something"))
	require.True(t, strings.Contains(q, "foo"))
}
