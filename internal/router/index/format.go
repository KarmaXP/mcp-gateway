// Package index builds deterministic catalog text for embeddings (plan §3.B.3 — fixed template).
package index

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const TemplateVersion = "v1"

// ToolRow is one tool from an aggregated MCP tools/list JSON result.
type ToolRow struct {
	Name        string
	Description string
	ParamKeys   []string
}

// FormatDocument renders the canonical text stored per tool (reproducible; thesis §4.5).
func FormatDocument(t ToolRow) string {
	sort.Strings(t.ParamKeys)
	params := strings.Join(t.ParamKeys, ", ")
	if params == "" {
		params = "(none)"
	}
	return fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s\nTemplate: %s",
		t.Name, t.Description, params, TemplateVersion)
}

// FormatQuery builds embedding text for a tools/call request (plan §3.B.3 fixed order).
func FormatQuery(toolName string, intent string, argumentKeys []string) string {
	sort.Strings(argumentKeys)
	ak := strings.Join(argumentKeys, ", ")
	if ak == "" {
		ak = "(none)"
	}
	intent = strings.TrimSpace(intent)
	if intent == "" {
		intent = "(none)"
	}
	return fmt.Sprintf("Intent: %s\nToolName: %s\nArgumentKeys: %s", intent, toolName, ak)
}

// ParseToolsListJSON extracts tool rows from tools/list `result` JSON.
func ParseToolsListJSON(raw []byte) ([]ToolRow, error) {
	var wrap struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("index: parse tools/list: %w", err)
	}
	out := make([]ToolRow, 0, len(wrap.Tools))
	for _, t := range wrap.Tools {
		keys := propertyKeys(t.InputSchema)
		out = append(out, ToolRow{
			Name:        t.Name,
			Description: t.Description,
			ParamKeys:   keys,
		})
	}
	return out, nil
}

func propertyKeys(schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	return keys
}
