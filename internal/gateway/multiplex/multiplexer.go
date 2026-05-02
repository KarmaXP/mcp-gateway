// Package multiplex fans out host JSON-RPC to MCP upstreams: merged initialize/tools/list,
// optional semantic routing and schema validation, and tools/call forwarding with namespacing.
package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
	"github.com/KarmaXP/mcp-gateway/internal/validate"
)

var errNoUpstreamsResponded = errors.New("multiplex: no upstreams responded to initialize")

const jsonNullLiteral = "null"

var emptyToolArguments = json.RawMessage(`{}`)

type Multiplexer struct {
	upstreams []backend.Upstream
	byPrefix  map[string]backend.Upstream

	initTimeout time.Duration
	listTimeout time.Duration
	callTimeout time.Duration

	mu         sync.RWMutex
	cachedList json.RawMessage
	cachedAt   time.Time
	listTTL    time.Duration

	semantic *router.SemanticRouter

	policyHolder *policy.Holder
	argLimits    validate.Limits

	catMu  sync.RWMutex
	catVer string

	schemaMu       sync.RWMutex
	toolValidators map[string]*jsonschema.Schema
}

type Option func(*Multiplexer)

func WithInitTimeout(d time.Duration) Option {
	return func(a *Multiplexer) { a.initTimeout = d }
}

func WithListTimeout(d time.Duration) Option {
	return func(a *Multiplexer) { a.listTimeout = d }
}

func WithCallTimeout(d time.Duration) Option {
	return func(a *Multiplexer) { a.callTimeout = d }
}

func WithListTTL(d time.Duration) Option {
	return func(a *Multiplexer) { a.listTTL = d }
}

func WithSemanticRouter(sr *router.SemanticRouter) Option {
	return func(a *Multiplexer) { a.semantic = sr }
}

// WithPolicyHolder attaches a reloadable policy holder (elevated-tool schema rules, SEC3).
func WithPolicyHolder(h *policy.Holder) Option {
	return func(a *Multiplexer) { a.policyHolder = h }
}

// WithPolicyEngine attaches a static policy engine (convenience wrapper around WithPolicyHolder).
func WithPolicyEngine(p *policy.Engine) Option {
	return func(a *Multiplexer) {
		if p == nil {
			a.policyHolder = nil
			return
		}
		a.policyHolder = policy.NewHolder(p)
	}
}

// WithArgumentValidateLimits overrides defaults for tools/call JSON argument bounds.
func WithArgumentValidateLimits(l validate.Limits) Option {
	return func(a *Multiplexer) { a.argLimits = l }
}

func New(upstreams []backend.Upstream, opts ...Option) (*Multiplexer, error) {
	byPrefix := make(map[string]backend.Upstream, len(upstreams))
	for _, b := range upstreams {
		p := b.Prefix()
		if err := namespace.ValidatePrefix(p); err != nil {
			return nil, fmt.Errorf("multiplex: validate prefix: %w", err)
		}
		if _, dup := byPrefix[p]; dup {
			return nil, fmt.Errorf("multiplex: duplicate prefix %q", p)
		}
		byPrefix[p] = b
	}
	a := &Multiplexer{
		upstreams:   append([]backend.Upstream(nil), upstreams...),
		byPrefix:    byPrefix,
		initTimeout: defaults.MultiplexInitTimeout,
		listTimeout: defaults.MultiplexListTimeout,
		callTimeout: defaults.MultiplexCallTimeout,
		listTTL:     defaults.MultiplexListCacheTTL,
	}
	for _, o := range opts {
		o(a)
	}
	dl := validate.DefaultLimits()
	if a.argLimits.MaxBytes <= 0 {
		a.argLimits.MaxBytes = dl.MaxBytes
	}
	if a.argLimits.MaxDepth <= 0 {
		a.argLimits.MaxDepth = dl.MaxDepth
	}
	if a.argLimits.MaxKeys <= 0 {
		a.argLimits.MaxKeys = dl.MaxKeys
	}
	return a, nil
}

func (a *Multiplexer) PrefixToUpstreamID() map[string]string {
	m := make(map[string]string, len(a.upstreams))
	for _, b := range a.upstreams {
		m[b.Prefix()] = b.ID()
	}
	return m
}

func (a *Multiplexer) Initialize(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexInit)
	defer span.End()

	results := make([]json.RawMessage, len(a.upstreams))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(tctx)
	for i, b := range a.upstreams {
		i, b := i, b
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(ctx, a.initTimeout)
			defer cancel()
			subID := json.RawMessage(fmt.Sprintf(`"gw-init-%s"`, b.ID()))
			req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "initialize", ID: subID, Params: hostParams()}
			resp, err := b.Call(callCtx, req)
			if err != nil {
				slog.Warn("initialize backend failed", "backend_id", b.ID(), "err", err)
				return nil
			}
			if resp.Error != nil {
				slog.Warn("initialize backend jsonrpc error", "backend_id", b.ID(), "code", resp.Error.Code, "message", resp.Error.Message)
				return nil
			}
			mu.Lock()
			results[i] = append(json.RawMessage(nil), resp.Result...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("multiplex: initialize upstreams: %w", err)
	}

	merged, err := mergeInitializeResults(results, a.upstreams)
	if err != nil {
		return rpc.NewError(hostID, errcodes.GatewayInternal, "gateway: all upstreams failed initialize", nil), nil
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal initialize result: %w", err)
	}
	a.invalidateToolCache()
	return rpc.NewResult(hostID, raw), nil
}

func hostParams() json.RawMessage {
	p := map[string]any{
		"protocolVersion": mcpwire.MCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    mcpwire.GatewayClientName,
			"version": mcpwire.GatewayClientVersion,
		},
	}
	b, _ := json.Marshal(p)
	return b
}

func mergeInitializeResults(results []json.RawMessage, upstreams []backend.Upstream) (map[string]any, error) {
	merged := map[string]any{
		"protocolVersion": mcpwire.MCPProtocolVersion,
		"capabilities":    map[string]any{},
		"serverInfo": map[string]any{
			"name":    mcpwire.GatewayClientName,
			"version": mcpwire.GatewayClientVersion,
			"extras": map[string]any{
				"backends": []string{},
			},
		},
	}
	capRoot := merged["capabilities"].(map[string]any)
	backendsList := merged["serverInfo"].(map[string]any)["extras"].(map[string]any)["backends"].([]string)

	anyOK := false
	for i, b := range upstreams {
		if len(results[i]) == 0 {
			continue
		}
		var one map[string]any
		if err := json.Unmarshal(results[i], &one); err != nil {
			slog.Warn("initialize merge: skip backend", "backend_id", b.ID(), "err", err)
			continue
		}
		anyOK = true
		if pv, ok := one["protocolVersion"].(string); ok && pv != "" {
			merged["protocolVersion"] = pv
		}
		shallowMerge(capRoot, one["capabilities"])
		backendsList = append(backendsList, b.ID())
	}
	if !anyOK {
		return nil, errNoUpstreamsResponded
	}
	merged["serverInfo"].(map[string]any)["extras"].(map[string]any)["backends"] = backendsList
	return merged, nil
}

func shallowMerge(dst map[string]any, src any) {
	sm, ok := src.(map[string]any)
	if !ok {
		return
	}
	for k, v := range sm {
		if _, exists := dst[k]; !exists {
			dst[k] = v
			continue
		}
		dm, dOK := dst[k].(map[string]any)
		vm, vOK := v.(map[string]any)
		if dOK && vOK {
			shallowMerge(dm, vm)
		}
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (a *Multiplexer) invalidateToolCache() {
	a.mu.Lock()
	a.cachedList = nil
	a.mu.Unlock()
	a.schemaMu.Lock()
	a.toolValidators = nil
	a.schemaMu.Unlock()
}

func coalesceArgs(a json.RawMessage) json.RawMessage {
	if len(a) == 0 || string(a) == jsonNullLiteral {
		return emptyToolArguments
	}
	return a
}

func (a *Multiplexer) semanticRoutingSignal(ctx context.Context, toolName string, args json.RawMessage) router.RoutingSignal {
	a.catMu.RLock()
	ver := a.catVer
	a.catMu.RUnlock()
	allowedList := hostctx.AllowedToolNamesFromContext(ctx)
	return router.RoutingSignal{
		Method:         "tools/call",
		ToolName:       toolName,
		ArgumentsJSON:  args,
		IntentText:     hostctx.ClientIntentFromContext(ctx),
		AllowedTools:   allowedList,
		CatalogVersion: ver,
	}
}
