package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

type Backend struct {
	id         string
	prefix     string
	toolNames  []string
	lastNative string

	ToolsCallDelay time.Duration
	ToolsCallErr   error

	InputSchemaByTool map[string]map[string]any

	mu sync.Mutex
}

func New(id, prefix string, toolNames []string) *Backend {
	return &Backend{id: id, prefix: prefix, toolNames: append([]string(nil), toolNames...)}
}

func (b *Backend) ID() string     { return b.id }
func (b *Backend) Prefix() string { return b.prefix }

func (b *Backend) LastNativeTool() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastNative
}

func (b *Backend) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	switch req.Method {
	case "initialize":
		return b.initialize(ctx, req)
	case "tools/list":
		return b.toolsList(ctx, req)
	case "tools/call":
		return b.toolsCall(ctx, req)
	default:
		return rpc.NewError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method), nil), nil
	}
}

func (b *Backend) initialize(_ context.Context, req *rpc.Request) (*rpc.Response, error) {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
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

func (b *Backend) toolsList(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	_ = ctx
	tools := make([]map[string]any, 0, len(b.toolNames))
	for _, n := range b.toolNames {
		schema := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
		if b.InputSchemaByTool != nil {
			if s, ok := b.InputSchemaByTool[n]; ok && s != nil {
				schema = s
			}
		}
		tools = append(tools, map[string]any{
			"name":        n,
			"description": "mock tool " + n,
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

func (b *Backend) toolsCall(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	b.mu.Lock()
	errCall := b.ToolsCallErr
	delay := b.ToolsCallDelay
	b.mu.Unlock()
	if errCall != nil {
		return nil, errCall
	}
	if delay > 0 {
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
			return rpc.NewError(req.ID, -32602, "invalid params", nil), nil
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
		return rpc.NewError(req.ID, -32602, fmt.Sprintf("unknown tool %q", params.Name), nil), nil
	}

	content := []map[string]any{
		{"type": "text", "text": fmt.Sprintf("ok from %s:%s", b.id, params.Name)},
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

var _ backend.Backend = (*Backend)(nil)
