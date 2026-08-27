// Command mcp_host_demo is a minimal reproducible MCP host client for the gateway.
// It drives: GET /mcp/sse -> initialize -> notifications/initialized -> tools/list -> tools/call.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/mcphostclient"
)

const (
	defaultGatewayURL = "http://127.0.0.1:8080"
	defaultTimeout = 15 * time.Second

	exitStatusError = 1
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mcp_host_demo error:", err)
		os.Exit(exitStatusError)
	}
}

func run() error {
	baseURL := strings.TrimSpace(os.Getenv("GATEWAY_URL"))
	if baseURL == "" {
		baseURL = defaultGatewayURL
	}

	toolNameOverride := strings.TrimSpace(os.Getenv("TOOL_NAME"))
	toolArgs, err := parseToolArgs(os.Getenv("TOOL_ARGS"))
	if err != nil {
		return fmt.Errorf("parse TOOL_ARGS: %w", err)
	}

	bearer := strings.TrimSpace(os.Getenv("GATEWAY_JWT"))

	ctx := context.Background()
	conn, err := mcphostclient.Dial(ctx, &http.Client{Timeout: 0}, baseURL, bearer)
	if err != nil {
		return fmt.Errorf("open sse: %w", err)
	}
	defer conn.Close()
	fmt.Println("session:", conn.SessionID())

	initializeRaw, err := conn.Call(ctx, mcpwire.MethodInitialize, map[string]any{
		"protocolVersion": mcpwire.MCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "mcp-host-demo", "version": "1.0.0"},
	}, defaultTimeout)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Println("initialize response:", string(initializeRaw))

	if err := conn.Notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	fmt.Println("sent notifications/initialized")

	toolsListRaw, err := conn.Call(ctx, mcpwire.MethodToolsList, nil, defaultTimeout)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	fmt.Println("tools/list response:", string(toolsListRaw))

	toolName, err := chooseToolName(toolsListRaw, toolNameOverride)
	if err != nil {
		return fmt.Errorf("choose tool name: %w", err)
	}
	fmt.Println("tools/call name:", toolName)

	toolsCallRaw, err := conn.Call(ctx, mcpwire.MethodToolsCall, map[string]any{
		"name":      toolName,
		"arguments": toolArgs,
	}, defaultTimeout)
	if err != nil {
		return fmt.Errorf("tools/call: %w", err)
	}
	fmt.Println("tools/call response:", string(toolsCallRaw))

	return nil
}

func parseToolArgs(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("TOOL_ARGS must be a JSON object: %w", err)
	}
	return args, nil
}

func chooseToolName(toolsListRaw []byte, override string) (string, error) {
	var response struct {
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(toolsListRaw, &response); err != nil {
		return "", fmt.Errorf("decode tools/list response: %w", err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("tools/list jsonrpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	if override != "" {
		for _, tool := range response.Result.Tools {
			if tool.Name == override {
				return override, nil
			}
		}
		return "", fmt.Errorf("tool %q not present in tools/list", override)
	}
	if len(response.Result.Tools) == 0 {
		return "", fmt.Errorf("tools/list returned no tools")
	}
	return response.Result.Tools[0].Name, nil
}
