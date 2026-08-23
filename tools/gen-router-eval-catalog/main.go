// Command gen-router-eval-catalog writes docs/evaluation/router-eval-catalog.json from routertest.SyntheticCatalog().
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/KarmaXP/mcp-gateway/internal/routertest"
)

func main() {
	cat := routertest.SyntheticCatalog()
	tools := make([]map[string]any, 0, len(cat))
	for _, entry := range cat {
		props := make(map[string]any, len(entry.ToolRow.ParamKeys))
		for _, key := range entry.ToolRow.ParamKeys {
			props[key] = map[string]any{"type": "string"}
		}
		tools = append(tools, map[string]any{
			"name":        entry.ToolRow.Name,
			"description": entry.ToolRow.Description,
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": props,
			},
		})
	}
	payload := map[string]any{"tools": tools}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
