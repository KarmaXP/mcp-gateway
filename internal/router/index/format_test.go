package index

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatDocumentStable(t *testing.T) {
	a := FormatDocument(Tool{Name: "alpha__echo", Description: "d", ParamKeys: []string{"z", "a"}})
	b := FormatDocument(Tool{Name: "alpha__echo", Description: "d", ParamKeys: []string{"a", "z"}})
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

func TestToolRowsFromListMapsMatchesParseToolsListJSON(t *testing.T) {
	raw := []byte(`{"tools":[{"name":"a__one","description":"d1","inputSchema":{"properties":{"z":{},"a":{}}}},{"name":"b__two","description":"","inputSchema":null}]}`)
	parsed, err := ParseToolsListJSON(raw)
	require.NoError(t, err)
	var wrap struct {
		Tools []map[string]any `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(raw, &wrap))
	fromMaps := ToolRowsFromListMaps(wrap.Tools)
	require.Len(t, fromMaps, len(parsed))
	for i := range parsed {
		require.Equal(t, parsed[i].Name, fromMaps[i].Name, "row %d name", i)
		require.Equal(t, parsed[i].Description, fromMaps[i].Description, "row %d description", i)
		require.ElementsMatch(t, parsed[i].ParamKeys, fromMaps[i].ParamKeys, "row %d param keys", i)
	}
}

func TestFormatQueryIncludesIntent(t *testing.T) {
	q := FormatQuery("foo", "do something", []string{"b", "a"})
	require.True(t, strings.Contains(q, "do something"))
	require.True(t, strings.Contains(q, "foo"))
}
