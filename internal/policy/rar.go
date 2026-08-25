package policy

import (
	"encoding/json"
	"fmt"
	"strings"
)

const rarTypeMCPTool = "mcp_tool"

type rarDetail struct {
	Type        string `json:"type"`
	ToolName    string `json:"tool_name"`
	ToolPattern string `json:"tool_pattern"`
}

// RAR authorization_details → namespaced ids; patterns support * and ?.
func expandAuthorizationDetails(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("policy: authorization_details: %w", err)
	}
	out := make([]string, 0, len(arr))
	for i, elem := range arr {
		var d rarDetail
		if err := json.Unmarshal(elem, &d); err != nil {
			return nil, fmt.Errorf("policy: authorization_details[%d]: %w", i, err)
		}
		if d.Type != rarTypeMCPTool {
			continue
		}
		name := strings.TrimSpace(d.ToolName)
		pat := strings.TrimSpace(d.ToolPattern)
		if name != "" && pat != "" {
			return nil, fmt.Errorf("policy: authorization_details[%d]: tool_name and tool_pattern are mutually exclusive", i)
		}
		if name != "" {
			out = append(out, name)
			continue
		}
		if pat != "" {
			out = append(out, pat)
			continue
		}
		return nil, fmt.Errorf("policy: authorization_details[%d]: mcp_tool entry requires tool_name or tool_pattern", i)
	}
	return dedupeStrings(out), nil
}

func ValidateToolPattern(pattern string) error {
	if i := strings.IndexAny(pattern, `[]\`); i >= 0 {
		return fmt.Errorf("policy: tool pattern %q: %q is not supported, use only * and ?", pattern, pattern[i:i+1])
	}
	return nil
}

func matchTool(namespacedTool, entry string) (bool, error) {
	if namespacedTool == "" || entry == "" {
		return false, nil
	}
	if err := ValidateToolPattern(entry); err != nil {
		return false, err
	}
	if !strings.ContainsAny(entry, "*?") {
		return namespacedTool == entry, nil
	}
	return matchToolPattern(namespacedTool, entry), nil
}

// Tool names are not paths: * spans the whole name, separators included.
func matchToolPattern(name, pattern string) bool {
	ni, pi := 0, 0
	starP, starN := -1, 0
	for ni < len(name) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == name[ni]):
			ni++
			pi++
		case pi < len(pattern) && pattern[pi] == '*':
			starP = pi
			starN = ni
			pi++
		case starP >= 0:
			starN++
			ni = starN
			pi = starP + 1
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

func anyEntryMatchesTool(tool string, entries []string) (bool, error) {
	for _, e := range entries {
		ok, err := matchTool(tool, e)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
