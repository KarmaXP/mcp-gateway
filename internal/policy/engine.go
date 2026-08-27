package policy

import (
	"fmt"
	"log/slog"
	"strings"
)

type Engine struct {
	version             string
	elevated            []string
	toolGroups          map[string][]string
	allowOnRARParseFail bool
	allowOpenSchemas    bool
}

type EngineInput struct {
	Version                string
	ElevatedTools          []string
	ToolGroups             map[string][]string
	AllowOnRARParseFailure bool
	AllowOpenSchemas       bool
}

func NewEngine(s EngineInput) *Engine {
	e := make([]string, 0, len(s.ElevatedTools))
	for _, t := range s.ElevatedTools {
		t = strings.TrimSpace(t)
		if t != "" {
			e = append(e, t)
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
		version:             ver,
		elevated:            e,
		toolGroups:          groups,
		allowOnRARParseFail: s.AllowOnRARParseFailure,
		allowOpenSchemas:    s.AllowOpenSchemas,
	}
}

func (e *Engine) Version() string {
	if e == nil {
		return ""
	}
	return e.version
}

func (e *Engine) HardenSchemas() bool {
	return e == nil || !e.allowOpenSchemas
}

// Elevated tools must have a compiled input schema.
func (e *Engine) RequiresInputSchema(namespacedTool string) bool {
	if e == nil || namespacedTool == "" {
		return false
	}
	ok, err := anyEntryMatchesTool(namespacedTool, e.elevated)
	if err != nil {
		return true
	}
	return ok
}

// Merged JWT and RAR allow list, never nil: a principal that authorizes nothing gets deny-all.
// The full catalog is requested explicitly, with an "*" entry.
func (e *Engine) EffectiveAllowList(c ClaimsInput) ([]string, error) {
	if c == nil {
		return []string{}, nil
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
		if !eng.allowOnRARParseFail {
			return nil, err
		}
		slog.Warn("policy: authorization_details parse failed, degraded to deny-all", "err", err)
		return []string{}, nil
	}
	switch {
	case len(base) > 0 && len(rarEntries) > 0:
		return intersectAllowEntries(base, rarEntries)
	case len(rarEntries) > 0:
		return rarEntries, nil
	case len(base) > 0:
		return base, nil
	default:
		return []string{}, nil
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
