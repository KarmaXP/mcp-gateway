package policy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
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
	e := NewEngine(EngineInput{Version: "v1"})
	got, err := e.EffectiveAllowList(fakeClaims{tools: []string{"a__x", "b__y"}})
	require.NoError(t, err)
	require.Equal(t, []string{"a__x", "b__y"}, got)
}

func TestEngine_EffectiveAllowList_RAROnly(t *testing.T) {
	e := NewEngine(EngineInput{Version: "v1"})
	raw, _ := json.Marshal([]map[string]string{
		{"type": "mcp_tool", "tool_name": "k8s__logs"},
	})
	got, err := e.EffectiveAllowList(fakeClaims{authDetails: raw})
	require.NoError(t, err)
	require.Equal(t, []string{"k8s__logs"}, got)
}

func TestEngine_EffectiveAllowList_EmptyIntersectionDenyAll(t *testing.T) {
	e := NewEngine(EngineInput{Version: "v1"})
	raw, _ := json.Marshal([]map[string]string{
		{"type": "mcp_tool", "tool_name": "rar__only"},
	})
	got, err := e.EffectiveAllowList(fakeClaims{
		tools:       []string{"jwt__only"},
		authDetails: raw,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)

	ok, err := AllowListPermits("jwt__only", got)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEngine_EffectiveAllowList_Intersection(t *testing.T) {
	e := NewEngine(EngineInput{Version: "v1"})
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
	e := NewEngine(EngineInput{
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
	e := NewEngine(EngineInput{Version: "v1"})
	_, err := e.EffectiveAllowList(fakeClaims{groups: []string{"missing"}})
	require.Error(t, err)
}

func TestEngine_EffectiveAllowList_RejectsEmptyRARToolEntry(t *testing.T) {
	e := NewEngine(EngineInput{Version: "v1"})
	raw, _ := json.Marshal([]map[string]string{
		{"type": "mcp_tool"},
	})
	_, err := e.EffectiveAllowList(fakeClaims{authDetails: raw})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires tool_name or tool_pattern")
}

func TestEngine_RARParseDegradeGrantsNothing(t *testing.T) {
	e := NewEngine(EngineInput{
		Version:                "v1",
		AllowOnRARParseFailure: true,
	})
	got, err := e.EffectiveAllowList(fakeClaims{
		tools:       []string{"a__x"},
		authDetails: json.RawMessage(`not-json`),
	})
	require.NoError(t, err, "degradation keeps the request alive")
	require.Equal(t, []string{}, got, "a malformed RAR must not promote the principal to its wider JWT list")
}

func TestEngine_RequiresStrictSchema(t *testing.T) {
	e := NewEngine(EngineInput{
		ElevatedTools: []string{"a__danger"},
	})
	require.True(t, e.RequiresInputSchema("a__danger"))
	require.False(t, e.RequiresInputSchema("a__safe"))
}

func TestEngine_HardenSchemas(t *testing.T) {
	e := NewEngine(EngineInput{})
	require.True(t, e.HardenSchemas())
}

func TestEffectiveAllowListDeniesWhenNothingIsAuthorized(t *testing.T) {
	eng := NewEngine(EngineInput{Version: "v-test"})
	tests := []struct {
		name   string
		claims ClaimsInput
	}{
		{name: "no tools, no groups, no RAR", claims: fakeClaims{}},
		{name: "no claims at all", claims: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eng.EffectiveAllowList(tc.claims)
			require.NoError(t, err)
			require.NotNil(t, got, "a nil list means unrestricted downstream, which is the fail-open this closes")
			require.Empty(t, got)

			ok, err := AllowListPermits("alpha__echo", got)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}
