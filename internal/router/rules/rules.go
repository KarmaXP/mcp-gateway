package rules

import (
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
)

type Rules struct {
	Aliases      map[string]string
	SiloKeywords map[string]string
}

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

// NarrowAllowed applies silo keyword narrowing. The bool is true when a silo matched
// (including zero-tool results); false means allowed is unchanged and unrestricted for vector search.
func (r *Rules) NarrowAllowed(intent string, allowed []string, catalog []string) ([]string, bool) {
	if r == nil || len(r.SiloKeywords) == 0 {
		return allowed, false
	}
	intentLower := strings.ToLower(strings.TrimSpace(intent))
	if intentLower == "" {
		return allowed, false
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
		return allowed, false
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
		return out, true
	}

	out := make([]string, 0, len(allowed))
	for _, t := range allowed {
		if match(t) {
			out = append(out, t)
		}
	}
	return out, true
}
