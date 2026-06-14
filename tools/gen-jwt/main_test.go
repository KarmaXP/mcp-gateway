package main

import "testing"

func TestResolveAlias(t *testing.T) {
	cases := []struct {
		name  string
		long  string
		alias string
		want  string
	}{
		{name: "alias overrides long", long: "https://dev.local", alias: "https://tfm.local", want: "https://tfm.local"},
		{name: "long used when alias empty", long: "https://dev.local", alias: "", want: "https://dev.local"},
		{name: "alias whitespace falls back to long", long: "mcp-gateway", alias: "   ", want: "mcp-gateway"},
		{name: "both empty", long: "", alias: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAlias(tc.long, tc.alias); got != tc.want {
				t.Fatalf("resolveAlias(%q, %q) = %q, want %q", tc.long, tc.alias, got, tc.want)
			}
		})
	}
}
