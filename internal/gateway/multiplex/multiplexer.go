// Package multiplex fans out host JSON-RPC to MCP upstreams: merged initialize/tools/list,
// optional semantic routing and schema validation, and tools/call forwarding with namespacing.
package multiplex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

var errNoUpstreamsResponded = errors.New("multiplex: no upstreams responded to initialize")

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
		initTimeout: 5 * time.Second,
		listTimeout: 10 * time.Second,
		callTimeout: 60 * time.Second,
		listTTL:     30 * time.Second,
	}
	for _, o := range opts {
		o(a)
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
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "mcp-gateway",
			"version": "0.1.0",
		},
	}
	b, _ := json.Marshal(p)
	return b
}

func mergeInitializeResults(results []json.RawMessage, upstreams []backend.Upstream) (map[string]any, error) {
	merged := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"serverInfo": map[string]any{
			"name":    "mcp-gateway",
			"version": "0.1.0",
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

func (a *Multiplexer) ToolsList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexToolsList)
	defer span.End()

	allowed := hostctx.AllowedToolNamesFromContext(tctx)

	if a.listTTL > 0 && len(allowed) == 0 {
		a.mu.RLock()
		if len(a.cachedList) > 0 && time.Since(a.cachedAt) < a.listTTL {
			c := append(json.RawMessage(nil), a.cachedList...)
			a.mu.RUnlock()
			a.refreshToolSchemasFromListJSON(c)
			return rpc.NewResult(hostID, c), nil
		}
		a.mu.RUnlock()
	}

	type listResult struct {
		tools []map[string]any
	}
	results := make([]listResult, len(a.upstreams))
	g, gctx := errgroup.WithContext(tctx)
	var mu sync.Mutex

	for i, b := range a.upstreams {
		i, b := i, b
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(gctx, a.listTimeout)
			defer cancel()
			subID := json.RawMessage(fmt.Sprintf(`"gw-list-%s"`, b.ID()))
			req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "tools/list", ID: subID, Params: nil}
			resp, err := b.Call(callCtx, req)
			if err != nil {
				slog.Warn("tools/list backend failed", "backend_id", b.ID(), "err", err)
				return nil
			}
			if resp.Error != nil {
				slog.Warn("tools/list jsonrpc error", "backend_id", b.ID(), "message", resp.Error.Message)
				return nil
			}
			var body struct {
				Tools []map[string]any `json:"tools"`
			}
			if err := json.Unmarshal(resp.Result, &body); err != nil {
				slog.Warn("tools/list decode", "backend_id", b.ID(), "err", err)
				return nil
			}
			mu.Lock()
			results[i].tools = body.Tools
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("multiplex: tools/list upstreams: %w", err)
	}

	var merged []map[string]any
	for i, b := range a.upstreams {
		prefix := b.Prefix()
		tools := append([]map[string]any(nil), results[i].tools...)
		sort.Slice(tools, func(i, j int) bool {
			n1, _ := tools[i]["name"].(string)
			n2, _ := tools[j]["name"].(string)
			return n1 < n2
		})
		for _, t := range tools {
			name, _ := t["name"].(string)
			ns, err := namespace.Join(prefix, name)
			if err != nil {
				slog.Warn("skip tool (namespace)", "backend_id", b.ID(), "tool", name, "err", err)
				continue
			}
			clone := cloneMap(t)
			clone["name"] = ns
			merged = append(merged, clone)
		}
	}

	a.replaceToolSchemasFromMerged(merged)

	outFull, err := json.Marshal(map[string]any{"tools": merged})
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal tools/list: %w", err)
	}
	if a.listTTL > 0 && len(allowed) == 0 {
		a.mu.Lock()
		a.cachedList = append(json.RawMessage(nil), outFull...)
		a.cachedAt = time.Now()
		a.mu.Unlock()
	}

	if a.semantic != nil && a.semantic.Enabled() {
		ver := fmt.Sprintf("%x", sha256.Sum256(outFull))
		indexed, err := router.BuildIndexedTools(outFull, func(prefix string) (string, error) {
			b, ok := a.byPrefix[prefix]
			if !ok {
				return "", fmt.Errorf("unknown prefix %q", prefix)
			}
			return b.ID(), nil
		})
		if err != nil {
			slog.Warn("router catalog build skipped", "err", err)
		} else if err := a.semantic.Reindex(tctx, ver, indexed); err != nil {
			slog.Warn("router reindex failed", "err", err)
		} else {
			a.catMu.Lock()
			a.catVer = ver
			a.catMu.Unlock()
			telemetry.SetIndexedCatalogToolCount(int64(len(indexed)))
		}
	}

	toReturn := outFull
	if len(allowed) > 0 {
		filtered := filterToolsForPolicy(merged, allowed)
		filteredRaw, err := json.Marshal(map[string]any{"tools": filtered})
		if err != nil {
			return nil, fmt.Errorf("multiplex: marshal filtered tools/list: %w", err)
		}
		toReturn = filteredRaw
	}

	return rpc.NewResult(hostID, toReturn), nil
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

func (a *Multiplexer) ToolsCall(ctx context.Context, hostID json.RawMessage, params json.RawMessage) (*rpc.Response, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, "invalid tools/call params", nil), nil
	}

	if a.semantic != nil && a.semantic.Enabled() {
		rctx, span := telemetry.StartSpan(ctx, telemetry.SpanSemanticRouter)
		sig := a.semanticRoutingSignal(ctx, p.Name, p.Arguments)
		resolved, dec, err := a.semantic.ResolveToolsCall(rctx, sig)
		telemetry.RecordSemanticRouting(rctx, dec, err)
		if err != nil {
			span.RecordError(err)
			span.End()
			return rpc.NewError(hostID, errcodes.ToolRoutingAmbiguous, err.Error(), nil), nil
		}
		span.End()
		p.Name = resolved
	}

	allowed := hostctx.AllowedToolNamesFromContext(ctx)
	if err := enforceToolPolicy(allowed, p.Name); err != nil {
		return rpc.NewError(hostID, errcodes.RequestRejected, err.Error(), nil), nil
	}

	prefix, native, err := namespace.Split(p.Name)
	if err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil), nil
	}
	b, ok := a.byPrefix[prefix]
	if !ok {
		return rpc.NewError(hostID, errcodes.InvalidParams, fmt.Sprintf("unknown tool prefix in %q", p.Name), nil), nil
	}

	argsForForward := coalesceArgs(p.Arguments)
	if err := a.validateToolArgs(p.Name, argsForForward); err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil), nil
	}

	forwardParams, err := json.Marshal(map[string]any{
		"name":      native,
		"arguments": argsForForward,
	})
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal tools/call forward params: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
	bctx, bspan := telemetry.StartSpan(callCtx, telemetry.SpanBackendCall)
	defer bspan.End()
	bspan.SetAttributes(attribute.String("backend_id", b.ID()))
	req := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      hostID,
		Params:  forwardParams,
	}
	resp, err := b.Call(bctx, req)
	if err != nil {
		bspan.RecordError(err)
		return rpc.NewError(hostID, errcodes.GatewayInternal, "backend call failed", nil), nil
	}
	return resp, nil
}

func coalesceArgs(a json.RawMessage) json.RawMessage {
	if len(a) == 0 || string(a) == "null" {
		return json.RawMessage(`{}`)
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
