package index

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const TemplateVersion = "v1"

type ToolRow struct {
	Name        string
	Description string
	ParamKeys   []string
}

func FormatDocument(t ToolRow) string {
	sort.Strings(t.ParamKeys)
	params := strings.Join(t.ParamKeys, ", ")
	if params == "" {
		params = "(none)"
	}
	return fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s\nTemplate: %s",
		t.Name, t.Description, params, TemplateVersion)
}

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
