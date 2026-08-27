package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
)

// Behaviour is what a mock upstream answers with, fixed when the mock is built.
type Behaviour struct {
	ToolsCallDelay time.Duration
	ToolsCallErr   error

	InputSchemaByTool map[string]map[string]any
	CallTextByTool    map[string]string
	DescriptionByTool map[string]string

	SupportsResources bool
	SupportsPrompts   bool
	ResourceURIs      []string
	PromptNames       []string

	InitTransportErr   error
	InitJSONRPCMessage string

	ToolsListJSONRPCMessage     string
	ResourcesListJSONRPCMessage string
	PromptsListJSONRPCMessage   string
}

type MockUpstream struct {
	id        string
	prefix    string
	toolNames []string
	behaviour Behaviour

	mu         sync.Mutex
	lastNative string

	toolsCallInvocations atomic.Uint64
	toolsListInvocations atomic.Uint64
}

func NewMockUpstream(id, prefix string, toolNames []string) *MockUpstream {
	return NewMockUpstreamWith(id, prefix, toolNames, Behaviour{})
}

// NewMockUpstreamWith builds a mock whose behaviour cannot change once it has been handed over.
func NewMockUpstreamWith(id, prefix string, toolNames []string, behaviour Behaviour) *MockUpstream {
	behaviour.InputSchemaByTool = cloneSchemas(behaviour.InputSchemaByTool)
	behaviour.CallTextByTool = cloneStrings(behaviour.CallTextByTool)
	behaviour.DescriptionByTool = cloneStrings(behaviour.DescriptionByTool)
	behaviour.ResourceURIs = append([]string(nil), behaviour.ResourceURIs...)
	behaviour.PromptNames = append([]string(nil), behaviour.PromptNames...)
	return &MockUpstream{
		id:        id,
		prefix:    prefix,
		toolNames: append([]string(nil), toolNames...),
		behaviour: behaviour,
	}
}

func cloneSchemas(in map[string]map[string]any) map[string]map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStrings(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (b *MockUpstream) ID() string     { return b.id }
func (b *MockUpstream) Prefix() string { return b.prefix }

func (b *MockUpstream) LastNativeTool() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastNative
}

// ToolsCallInvocationCount is the number of tools/call JSON-RPC requests accepted by this mock (after authz in the gateway).
func (b *MockUpstream) ToolsCallInvocationCount() uint64 {
	if b == nil {
		return 0
	}
	return b.toolsCallInvocations.Load()
}

// ToolsListInvocationCount is the number of tools/list JSON-RPC requests handled by this mock.
func (b *MockUpstream) ToolsListInvocationCount() uint64 {
	if b == nil {
		return 0
	}
	return b.toolsListInvocations.Load()
}

func (b *MockUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	switch req.Method {
	case mcpwire.MethodInitialize:
		return b.initialize(ctx, req)
	case mcpwire.MethodToolsList:
		return b.toolsList(ctx, req)
	case mcpwire.MethodToolsCall:
		return b.toolsCall(ctx, req)
	case mcpwire.MethodResourcesList:
		return b.resourcesList(ctx, req)
	case mcpwire.MethodResourcesRead:
		return b.resourcesRead(ctx, req)
	case mcpwire.MethodPromptsList:
		return b.promptsList(ctx, req)
	case mcpwire.MethodPromptsGet:
		return b.promptsGet(ctx, req)
	default:
		return rpc.NewError(req.ID, errcodes.MethodNotFound, fmt.Sprintf("method not found: %s", req.Method), nil), nil
	}
}

func (b *MockUpstream) initialize(_ context.Context, req *rpc.Request) (*rpc.Response, error) {
	if b.behaviour.InitTransportErr != nil {
		return nil, b.behaviour.InitTransportErr
	}
	jsonrpcMessage := strings.TrimSpace(b.behaviour.InitJSONRPCMessage)
	if jsonrpcMessage != "" {
		return rpc.NewError(req.ID, errcodes.InternalError, jsonrpcMessage, nil), nil
	}
	caps := map[string]any{
		"tools": map[string]any{},
	}
	if b.behaviour.SupportsResources {
		caps["resources"] = map[string]any{}
	}
	if b.behaviour.SupportsPrompts {
		caps["prompts"] = map[string]any{}
	}
	result := map[string]any{
		"protocolVersion": mcpwire.MCPProtocolVersion,
		"capabilities":    caps,
		"serverInfo": map[string]any{
			"name": b.id,
		},
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

func (b *MockUpstream) toolsList(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	_ = ctx
	b.toolsListInvocations.Add(1)
	jsonrpcMessage := strings.TrimSpace(b.behaviour.ToolsListJSONRPCMessage)
	if jsonrpcMessage != "" {
		return rpc.NewError(req.ID, errcodes.InternalError, jsonrpcMessage, nil), nil
	}
	tools := make([]map[string]any, 0, len(b.toolNames))
	for _, n := range b.toolNames {
		schema := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
		if s, ok := b.behaviour.InputSchemaByTool[n]; ok && s != nil {
			schema = s
		}
		description := "mock tool " + n
		if d, ok := b.behaviour.DescriptionByTool[n]; ok && d != "" {
			description = d
		}
		tools = append(tools, map[string]any{
			"name":        n,
			"description": description,
			"inputSchema": schema,
		})
	}
	result := map[string]any{"tools": tools}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

func (b *MockUpstream) toolsCall(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	b.toolsCallInvocations.Add(1)
	if b.behaviour.ToolsCallErr != nil {
		return nil, b.behaviour.ToolsCallErr
	}
	if delay := b.behaviour.ToolsCallDelay; delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			//nolint:nilerr // a JSON-RPC error travels in the response; the error return means transport failure
			return rpc.NewError(req.ID, errcodes.InvalidParams, "invalid params", nil), nil
		}
	}
	b.mu.Lock()
	b.lastNative = params.Name
	b.mu.Unlock()

	valid := false
	for _, n := range b.toolNames {
		if n == params.Name {
			valid = true
			break
		}
	}
	if !valid {
		return rpc.NewError(req.ID, errcodes.InvalidParams, fmt.Sprintf("unknown tool %q", params.Name), nil), nil
	}

	text := fmt.Sprintf("ok from %s:%s", b.id, params.Name)
	if t, ok := b.behaviour.CallTextByTool[params.Name]; ok && t != "" {
		text = t
	}
	content := []map[string]any{
		{"type": "text", "text": text},
	}
	result := map[string]any{
		"content": content,
		"isError": false,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

func (b *MockUpstream) resourcesList(_ context.Context, req *rpc.Request) (*rpc.Response, error) {
	uris := b.behaviour.ResourceURIs
	jsonrpcMessage := strings.TrimSpace(b.behaviour.ResourcesListJSONRPCMessage)
	if !b.behaviour.SupportsResources {
		return rpc.NewError(req.ID, errcodes.MethodNotFound, "resources not supported", nil), nil
	}
	if jsonrpcMessage != "" {
		return rpc.NewError(req.ID, errcodes.InternalError, jsonrpcMessage, nil), nil
	}
	res := make([]map[string]any, 0, len(uris))
	for _, u := range uris {
		res = append(res, map[string]any{
			"uri":  u,
			"name": "res-" + u,
		})
	}
	raw, err := json.Marshal(map[string]any{"resources": res})
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

func (b *MockUpstream) resourcesRead(_ context.Context, req *rpc.Request) (*rpc.Response, error) {
	if !b.behaviour.SupportsResources {
		return rpc.NewError(req.ID, errcodes.MethodNotFound, "resources not supported", nil), nil
	}
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.URI == "" {
		//nolint:nilerr // a JSON-RPC error travels in the response; the error return means transport failure
		return rpc.NewError(req.ID, errcodes.InvalidParams, "uri required", nil), nil
	}
	raw, err := json.Marshal(map[string]any{
		"contents": []map[string]any{{
			"uri":      p.URI,
			"mimeType": "text/plain",
			"text":     fmt.Sprintf("body:%s", p.URI),
		}},
	})
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

func (b *MockUpstream) promptsList(_ context.Context, req *rpc.Request) (*rpc.Response, error) {
	names := b.behaviour.PromptNames
	jsonrpcMessage := strings.TrimSpace(b.behaviour.PromptsListJSONRPCMessage)
	if !b.behaviour.SupportsPrompts {
		return rpc.NewError(req.ID, errcodes.MethodNotFound, "prompts not supported", nil), nil
	}
	if jsonrpcMessage != "" {
		return rpc.NewError(req.ID, errcodes.InternalError, jsonrpcMessage, nil), nil
	}
	ps := make([]map[string]any, 0, len(names))
	for _, n := range names {
		ps = append(ps, map[string]any{
			"name":        n,
			"description": "mock prompt " + n,
		})
	}
	raw, err := json.Marshal(map[string]any{"prompts": ps})
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

func (b *MockUpstream) promptsGet(_ context.Context, req *rpc.Request) (*rpc.Response, error) {
	if !b.behaviour.SupportsPrompts {
		return rpc.NewError(req.ID, errcodes.MethodNotFound, "prompts not supported", nil), nil
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		//nolint:nilerr // a JSON-RPC error travels in the response; the error return means transport failure
		return rpc.NewError(req.ID, errcodes.InvalidParams, "name required", nil), nil
	}
	raw, err := json.Marshal(map[string]any{
		"description": "mock",
		"messages": []map[string]any{{
			"role": "user",
			"content": map[string]any{
				"type": "text",
				"text": fmt.Sprintf("prompt %s", p.Name),
			},
		}},
	})
	if err != nil {
		return nil, err
	}
	return rpc.NewResult(req.ID, raw), nil
}

var _ upstream.Client = (*MockUpstream)(nil)
