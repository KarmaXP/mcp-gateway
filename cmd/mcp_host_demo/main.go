// Command mcp_host_demo is a minimal reproducible MCP host client for the gateway.
// It drives: GET /mcp/sse -> initialize -> notifications/initialized -> tools/list -> tools/call.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

const (
	defaultGatewayURL = "http://127.0.0.1:8080"
	defaultTimeout = 15 * time.Second

	exitStatusError = 1

	sseChannelBuffer = 64
)

var idSeq atomic.Int64

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
	baseURL = strings.TrimRight(baseURL, "/")

	toolNameOverride := strings.TrimSpace(os.Getenv("TOOL_NAME"))

	toolArgs, err := parseToolArgs(os.Getenv("TOOL_ARGS"))
	if err != nil {
		return fmt.Errorf("parse TOOL_ARGS: %w", err)
	}

	// Required when the gateway runs with AUTH_MODE=jwt; sent on SSE and every POST.
	bearer := strings.TrimSpace(os.Getenv("GATEWAY_JWT"))

	client := &http.Client{Timeout: 0}
	ctx := context.Background()

	sid, events, cancel, err := openSSE(ctx, client, baseURL+"/mcp/sse", bearer)
	if err != nil {
		return fmt.Errorf("open sse: %w", err)
	}
	defer cancel()
	fmt.Println("session:", sid)

	initializeID := nextID()
	if err := postRPC(client, baseURL+"/mcp/rpc", sid, bearer, map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      initializeID,
		"method":  mcpwire.MethodInitialize,
		"params": map[string]any{
			"protocolVersion": mcpwire.MCPProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "mcp-host-demo",
				"version": "1.0.0",
			},
		},
	}); err != nil {
		return fmt.Errorf("initialize post: %w", err)
	}
	initializeRaw, err := waitJSONRPCByID(events, initializeID, defaultTimeout)
	if err != nil {
		return fmt.Errorf("wait initialize response: %w", err)
	}
	fmt.Println("initialize response:", string(initializeRaw))

	if err := postRPC(client, baseURL+"/mcp/rpc", sid, bearer, map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"method":  "notifications/initialized",
	}); err != nil {
		return fmt.Errorf("initialized notification post: %w", err)
	}
	fmt.Println("sent notifications/initialized")

	toolsListID := nextID()
	if err := postRPC(client, baseURL+"/mcp/rpc", sid, bearer, map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      toolsListID,
		"method":  mcpwire.MethodToolsList,
	}); err != nil {
		return fmt.Errorf("tools/list post: %w", err)
	}
	toolsListRaw, err := waitJSONRPCByID(events, toolsListID, defaultTimeout)
	if err != nil {
		return fmt.Errorf("wait tools/list response: %w", err)
	}
	fmt.Println("tools/list response:", string(toolsListRaw))

	toolName, err := chooseToolName(toolsListRaw, toolNameOverride)
	if err != nil {
		return fmt.Errorf("choose tool name: %w", err)
	}
	fmt.Println("tools/call name:", toolName)

	toolsCallID := nextID()
	if err := postRPC(client, baseURL+"/mcp/rpc", sid, bearer, map[string]any{
		"jsonrpc": rpc.JSONRPCVersion,
		"id":      toolsCallID,
		"method":  mcpwire.MethodToolsCall,
		"params": map[string]any{
			"name":      toolName,
			"arguments": toolArgs,
		},
	}); err != nil {
		return fmt.Errorf("tools/call post: %w", err)
	}
	toolsCallRaw, err := waitJSONRPCByID(events, toolsCallID, defaultTimeout)
	if err != nil {
		return fmt.Errorf("wait tools/call response: %w", err)
	}
	fmt.Println("tools/call response:", string(toolsCallRaw))

	return nil
}

func nextID() int64 {
	return idSeq.Add(1)
}

func openSSE(ctx context.Context, client *http.Client, endpoint, bearer string) (sid string, out <-chan string, cancel func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req) //nolint:bodyclose // Body is closed by goroutine on context cancellation.
	if err != nil {
		return "", nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		_ = resp.Body.Close()
		return "", nil, nil, fmt.Errorf("sse status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	sid = strings.TrimSpace(resp.Header.Get("Mcp-Session-Id"))
	if sid == "" {
		_ = resp.Body.Close()
		return "", nil, nil, fmt.Errorf("missing Mcp-Session-Id header")
	}

	ch := make(chan string, sseChannelBuffer)
	cctx, cfn := context.WithCancel(ctx)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		reader := bufio.NewReader(resp.Body)
		for {
			select {
			case <-cctx.Done():
				return
			default:
			}
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, mcpwire.SSEDataLinePrefix) {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, mcpwire.SSEDataLinePrefix))
			select {
			case ch <- payload:
			case <-cctx.Done():
				return
			}
		}
	}()
	return sid, ch, cfn, nil
}

func postRPC(client *http.Client, endpoint, sid, bearer string, reqBody map[string]any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", sid)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.MaxHTTPUpstreamErrorBody))
		return fmt.Errorf("rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func waitJSONRPCByID(events <-chan string, id int64, timeout time.Duration) ([]byte, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for id %d", id)
		case payload, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("sse closed while waiting for id %d", id)
			}
			if payload == "" {
				continue
			}
			raw := []byte(payload)
			if !matchesID(raw, id) {
				continue
			}
			return raw, nil
		}
	}
}

func matchesID(raw []byte, id int64) bool {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false
	}

	var numericID int64
	if err := json.Unmarshal(envelope.ID, &numericID); err == nil && numericID == id {
		return true
	}
	var stringID string
	if err := json.Unmarshal(envelope.ID, &stringID); err == nil && stringID == fmt.Sprintf("%d", id) {
		return true
	}
	return false
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
