package policy

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

const rarTypeMCPTool = "mcp_tool"

type rarDetail struct {
	Type        string `json:"type"`
	ToolName    string `json:"tool_name"`
	ToolPattern string `json:"tool_pattern"`
}

// RAR authorization_details → namespaced ids; globs use filepath.Match.
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

func MatchTool(namespacedTool, entry string) (bool, error) {
	if namespacedTool == "" || entry == "" {
		return false, nil
	}
	if !strings.ContainsAny(entry, "*?[]") {
		return namespacedTool == entry, nil
	}
	ok, err := filepath.Match(entry, namespacedTool)
	if err != nil {
		return false, fmt.Errorf("policy: glob %q: %w", entry, err)
	}
	return ok, nil
}

func anyEntryMatchesTool(tool string, entries []string) (bool, error) {
	for _, e := range entries {
		ok, err := MatchTool(tool, e)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}
