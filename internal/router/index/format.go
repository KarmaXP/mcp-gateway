package index

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const templateVersion = "v1"

type Tool struct {
	Name        string
	Description string
	ParamKeys   []string
}

func FormatDocument(t Tool) string {
	sort.Strings(t.ParamKeys)
	params := strings.Join(t.ParamKeys, ", ")
	if params == "" {
		params = "(none)"
	}
	return fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s\nTemplate: %s",
		t.Name, t.Description, params, templateVersion)
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

func ParseToolsListJSON(raw []byte) ([]Tool, error) {
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
	out := make([]Tool, 0, len(wrap.Tools))
	for _, t := range wrap.Tools {
		keys := inputSchemaPropertyKeys(t.InputSchema)
		out = append(out, Tool{
			Name:        t.Name,
			Description: t.Description,
			ParamKeys:   keys,
		})
	}
	return out, nil
}

func inputSchemaPropertyKeys(schema map[string]any) []string {
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

// When tools/list is already decoded as maps (avoids a second full JSON parse).
func ToolRowsFromListMaps(tools []map[string]any) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		sch, _ := t["inputSchema"].(map[string]any)
		out = append(out, Tool{
			Name:        name,
			Description: desc,
			ParamKeys:   inputSchemaPropertyKeys(sch),
		})
	}
	return out
}
