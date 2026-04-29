package multiplex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func filterToolsForPolicy(merged []map[string]any, allowed []string) []map[string]any {
	if len(allowed) == 0 {
		return merged
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, n := range allowed {
		allow[n] = struct{}{}
	}
	out := make([]map[string]any, 0, len(merged))
	for _, t := range merged {
		name, _ := t["name"].(string)
		if _, ok := allow[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

func toolSchemaURL(namespacedName string) string {
	return "https://mcp-gateway.local/tool/" + url.PathEscape(namespacedName)
}

func compileToolValidator(namespacedName string, schemaJSON json.RawMessage) (*jsonschema.Schema, error) {
	var doc any
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	loc := toolSchemaURL(namespacedName)
	if err := c.AddResource(loc, doc); err != nil {
		return nil, err
	}
	return c.Compile(loc)
}

func (a *Multiplexer) replaceToolSchemasFromMerged(merged []map[string]any) {
	out := make(map[string]*jsonschema.Schema)
	for _, t := range merged {
		name, _ := t["name"].(string)
		if name == "" {
			continue
		}
		sch, ok := t["inputSchema"]
		if !ok || sch == nil {
			continue
		}
		raw, err := json.Marshal(sch)
		if err != nil || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		v, err := compileToolValidator(name, raw)
		if err != nil {
			slog.Warn("tool inputSchema compile skipped", "tool", name, "err", err)
			continue
		}
		out[name] = v
	}
	a.schemaMu.Lock()
	a.toolValidators = out
	a.schemaMu.Unlock()
}

func (a *Multiplexer) refreshToolSchemasFromListJSON(raw json.RawMessage) {
	tools, err := parseToolsArrayFromListJSON(raw)
	if err != nil {
		return
	}
	a.replaceToolSchemasFromMerged(tools)
}

func parseToolsArrayFromListJSON(raw json.RawMessage) ([]map[string]any, error) {
	var body struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body.Tools, nil
}

func allowedContains(allowed []string, namespacedTool string) bool {
	for _, n := range allowed {
		if n == namespacedTool {
			return true
		}
	}
	return false
}

func enforceToolPolicy(allowed []string, namespacedTool string) error {
	if len(allowed) == 0 {
		return nil
	}
	if !allowedContains(allowed, namespacedTool) {
		return fmt.Errorf("tool %q not allowed for this token", namespacedTool)
	}
	return nil
}

func (a *Multiplexer) validateToolArgs(namespacedTool string, argsJSON json.RawMessage) error {
	a.schemaMu.RLock()
	sch := a.toolValidators[namespacedTool]
	a.schemaMu.RUnlock()
	if sch == nil {
		return nil
	}
	var inst any
	if err := json.Unmarshal(argsJSON, &inst); err != nil {
		return fmt.Errorf("invalid JSON arguments: %w", err)
	}
	if err := sch.Validate(inst); err != nil {
		return fmt.Errorf("arguments do not match tool schema: %w", err)
	}
	return nil
}
