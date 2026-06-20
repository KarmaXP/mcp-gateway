package policy

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/KarmaXP/mcp-gateway/internal/config"
)

type Engine struct {
	version         string
	elevated        map[string]struct{}
	toolGroups      map[string][]string
	allowOnEvalFail bool
	hardenSchemas   bool
}

func NewEngine(s config.PolicySettings) *Engine {
	e := make(map[string]struct{})
	for _, t := range s.ElevatedTools {
		t = strings.TrimSpace(t)
		if t != "" {
			e[t] = struct{}{}
		}
	}
	groups := make(map[string][]string, len(s.ToolGroups))
	for k, v := range s.ToolGroups {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		cp := make([]string, 0, len(v))
		for _, x := range v {
			x = strings.TrimSpace(x)
			if x != "" {
				cp = append(cp, x)
			}
		}
		if len(cp) > 0 {
			groups[k] = dedupeStrings(cp)
		}
	}
	ver := strings.TrimSpace(s.Version)
	if ver == "" {
		ver = "default"
	}
	return &Engine{
		version:         ver,
		elevated:        e,
		toolGroups:      groups,
		allowOnEvalFail: s.AllowOnEvalFailure,
		hardenSchemas:   s.HardenSchemas,
	}
}

func (e *Engine) Version() string {
	if e == nil {
		return ""
	}
	return e.version
}

func (e *Engine) HardenSchemas() bool {
	return e != nil && e.hardenSchemas
}

// Elevated tools must have a compiled input schema.
func (e *Engine) RequiresStrictSchema(namespacedTool string) bool {
	if e == nil || namespacedTool == "" {
		return false
	}
	_, ok := e.elevated[namespacedTool]
	return ok
}

// Merged JWT and RAR allow list; empty -> no restriction (full catalog for that principal).
func (e *Engine) EffectiveAllowList(c ClaimsInput) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	eng := e
	if eng == nil {
		eng = &Engine{}
	}
	base, err := eng.expandJWTAllowEntries(c)
	if err != nil {
		return nil, err
	}
	rarEntries, err := expandAuthorizationDetails(c.RawAuthorizationDetails())
	if err != nil {
		if !eng.allowOnEvalFail {
			return nil, err
		}
		slog.Warn("policy: authorization_details parse failed, degraded to JWT allow list only", "err", err)
		rarEntries = nil
	}
	switch {
	case len(base) > 0 && len(rarEntries) > 0:
		return intersectAllowEntries(base, rarEntries)
	case len(rarEntries) > 0:
		return rarEntries, nil
	case len(base) > 0:
		return base, nil
	default:
		return nil, nil
	}
}

func (e *Engine) expandJWTAllowEntries(c ClaimsInput) ([]string, error) {
	var out []string
	if tools := c.NormalizedMcpTools(); len(tools) > 0 {
		out = append(out, tools...)
	}
	for _, g := range c.NormalizedToolGroups() {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		expanded, ok := e.toolGroups[g]
		if !ok {
			return nil, fmt.Errorf("policy: unknown mcp_tool_groups entry %q", g)
		}
		out = append(out, expanded...)
	}
	return dedupeStrings(out), nil
}

func intersectAllowEntries(jwtEntries, rarEntries []string) ([]string, error) {
	var out []string
	for _, t := range jwtEntries {
		ok, err := anyEntryMatchesTool(t, rarEntries)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
	}
	out = dedupeStrings(out)
	if len(out) == 0 {
		return []string{}, nil
	}
	return out, nil
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
