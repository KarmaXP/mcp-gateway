// Package rules implements the deterministic layer between exact match and vector search:
// alias resolution and silo→prefix narrowing of AllowedTools (plan §3.B.6).
package rules

import (
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
)

// Rules holds optional alias and silo keyword maps (all case-insensitive for lookups).
type Rules struct {
	// Aliases maps a requested tool name (any case) to a canonical namespaced tool id.
	Aliases map[string]string
	// SiloKeywords maps a substring that may appear in intent text to a backend prefix
	// (the segment before namespace.Separator in namespaced tools).
	SiloKeywords map[string]string
}

// New builds a Rules value, copying the provided maps.
func New(aliases, siloKeywords map[string]string) *Rules {
	cp := func(m map[string]string) map[string]string {
		if len(m) == 0 {
			return nil
		}
		out := make(map[string]string, len(m))
		for k, v := range m {
			kk := strings.ToLower(strings.TrimSpace(k))
			if kk == "" || strings.TrimSpace(v) == "" {
				continue
			}
			out[kk] = strings.TrimSpace(v)
		}
		return out
	}
	return &Rules{
		Aliases:      cp(aliases),
		SiloKeywords: cp(siloKeywords),
	}
}

// CanonicalAlias returns the namespaced tool id for an alias key, or "" if none.
func (r *Rules) CanonicalAlias(requested string) string {
	if r == nil || len(r.Aliases) == 0 {
		return ""
	}
	k := strings.ToLower(strings.TrimSpace(requested))
	if k == "" {
		return ""
	}
	return r.Aliases[k]
}

// NarrowAllowed restricts the allow-list when intent text mentions a silo keyword.
// If allowed is empty (no host policy), narrowing becomes “all catalog tools in matching silos”.
// If allowed is non-empty, the result is the intersection with silo-matched tools.
func (r *Rules) NarrowAllowed(intent string, allowed []string, catalog []string) []string {
	if r == nil || len(r.SiloKeywords) == 0 {
		return allowed
	}
	intentLower := strings.ToLower(strings.TrimSpace(intent))
	if intentLower == "" {
		return allowed
	}

	var prefixes []string
	for kw, prefix := range r.SiloKeywords {
		if kw == "" {
			continue
		}
		if strings.Contains(intentLower, kw) {
			prefixes = append(prefixes, prefix)
		}
	}
	if len(prefixes) == 0 {
		return allowed
	}

	match := func(tool string) bool {
		p, _, err := namespace.Split(tool)
		if err != nil {
			return false
		}
		for _, pref := range prefixes {
			if p == pref {
				return true
			}
		}
		return false
	}

	if len(allowed) == 0 {
		out := make([]string, 0)
		for _, t := range catalog {
			if match(t) {
				out = append(out, t)
			}
		}
		return out
	}

	out := make([]string, 0, len(allowed))
	for _, t := range allowed {
		if match(t) {
			out = append(out, t)
		}
	}
	return out
}
