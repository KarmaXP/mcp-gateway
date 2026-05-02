package policy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/config"
)

type fakeClaims struct {
	sub         string
	tools       []string
	groups      []string
	authDetails json.RawMessage
}

func (f fakeClaims) Subject() string                          { return f.sub }
func (f fakeClaims) NormalizedMcpTools() []string             { return f.tools }
func (f fakeClaims) NormalizedToolGroups() []string           { return f.groups }
func (f fakeClaims) RawAuthorizationDetails() json.RawMessage { return f.authDetails }

func TestEngine_EffectiveAllowList_JWTOnly(t *testing.T) {
	e := NewEngine(config.PolicySettings{Version: "v1"})
	got, err := e.EffectiveAllowList(fakeClaims{tools: []string{"a__x", "b__y"}})
	require.NoError(t, err)
	require.Equal(t, []string{"a__x", "b__y"}, got)
}

func TestEngine_EffectiveAllowList_RAROnly(t *testing.T) {
	e := NewEngine(config.PolicySettings{Version: "v1"})
	raw, _ := json.Marshal([]map[string]string{
		{"type": "mcp_tool", "tool_name": "k8s__logs"},
	})
	got, err := e.EffectiveAllowList(fakeClaims{authDetails: raw})
	require.NoError(t, err)
	require.Equal(t, []string{"k8s__logs"}, got)
}

func TestEngine_EffectiveAllowList_Intersection(t *testing.T) {
	e := NewEngine(config.PolicySettings{Version: "v1"})
	raw, _ := json.Marshal([]map[string]string{
		{"type": "mcp_tool", "tool_pattern": "a__*"},
	})
	got, err := e.EffectiveAllowList(fakeClaims{
		tools:       []string{"a__one", "a__two", "b__nope"},
		authDetails: raw,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a__one", "a__two"}, got)
}

func TestEngine_ToolGroups(t *testing.T) {
	e := NewEngine(config.PolicySettings{
		Version: "v1",
		ToolGroups: map[string][]string{
			"read": {"a__x", "a__y"},
		},
	})
	got, err := e.EffectiveAllowList(fakeClaims{groups: []string{"read"}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a__x", "a__y"}, got)
}

func TestEngine_UnknownGroupFailsClosed(t *testing.T) {
	e := NewEngine(config.PolicySettings{Version: "v1"})
	_, err := e.EffectiveAllowList(fakeClaims{groups: []string{"missing"}})
	require.Error(t, err)
}

func TestEngine_RARParseDegrade(t *testing.T) {
	e := NewEngine(config.PolicySettings{
		Version:            "v1",
		AllowOnEvalFailure: true,
	})
	got, err := e.EffectiveAllowList(fakeClaims{
		tools:       []string{"a__x"},
		authDetails: json.RawMessage(`not-json`),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a__x"}, got)
}

func TestEngine_RequiresStrictSchema(t *testing.T) {
	e := NewEngine(config.PolicySettings{
		ElevatedTools: []string{"a__danger"},
	})
	require.True(t, e.RequiresStrictSchema("a__danger"))
	require.False(t, e.RequiresStrictSchema("a__safe"))
}
