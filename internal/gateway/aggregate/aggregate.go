// Package aggregate merges initialize and tools/list across backends and forwards tools/call.
package aggregate

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
	"github.com/KarmaXP/mcp-gateway/internal/gateway/ingress"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

var errNoBackends = errors.New("aggregate: no backends responded to initialize")

// Aggregator merges initialize and tools/list across backends in configured order (stable prefix order).
type Aggregator struct {
	backends []backend.Backend
	byPrefix map[string]backend.Backend

	initTimeout time.Duration
	listTimeout time.Duration
	callTimeout time.Duration

	mu         sync.RWMutex
	cachedList json.RawMessage
	cachedAt   time.Time
	listTTL    time.Duration

	semantic *router.Engine // optional semantic router

	catMu  sync.RWMutex
	catVer string // sha256 of last aggregated tools/list JSON (for optional client pinning)

	schemaMu       sync.RWMutex
	toolValidators map[string]*jsonschema.Schema // namespaced tool -> compiled inputSchema
}

// Option configures the aggregator.
type Option func(*Aggregator)

// WithInitTimeout sets per-backend initialize deadline.
func WithInitTimeout(d time.Duration) Option {
	return func(a *Aggregator) { a.initTimeout = d }
}

// WithListTimeout sets per-backend tools/list deadline.
func WithListTimeout(d time.Duration) Option {
	return func(a *Aggregator) { a.listTimeout = d }
}

// WithCallTimeout sets per-backend tools/call deadline.
func WithCallTimeout(d time.Duration) Option {
	return func(a *Aggregator) { a.callTimeout = d }
}

// WithListTTL sets cache TTL for aggregated tools/list (0 disables cache).
func WithListTTL(d time.Duration) Option {
	return func(a *Aggregator) { a.listTTL = d }
}

// WithSemanticRouter attaches the router engine (nil-safe; ModeOff is a no-op).
func WithSemanticRouter(e *router.Engine) Option {
	return func(a *Aggregator) { a.semantic = e }
}

// New builds an aggregator. Backend prefixes must be unique.
func New(backends []backend.Backend, opts ...Option) (*Aggregator, error) {
	byPrefix := make(map[string]backend.Backend, len(backends))
	for _, b := range backends {
		p := b.Prefix()
		if err := namespace.ValidatePrefix(p); err != nil {
			return nil, fmt.Errorf("aggregate: validate prefix: %w", err)
		}
		if _, dup := byPrefix[p]; dup {
			return nil, fmt.Errorf("aggregate: duplicate prefix %q", p)
		}
		byPrefix[p] = b
	}
	a := &Aggregator{
		backends:    append([]backend.Backend(nil), backends...),
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

// PrefixToBackendID returns the mapping used for tools/call resolution.
func (a *Aggregator) PrefixToBackendID() map[string]string {
	m := make(map[string]string, len(a.backends))
	for _, b := range a.backends {
		m[b.Prefix()] = b.ID()
	}
	return m
}

// Initialize fans out to all backends, omits per-backend failures, and errors only if every backend fails.
func (a *Aggregator) Initialize(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, "mcp.aggregate.initialize")
	defer span.End()

	results := make([]json.RawMessage, len(a.backends))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(tctx)
	for i, b := range a.backends {
		i, b := i, b
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(ctx, a.initTimeout)
			defer cancel()
			subID := json.RawMessage(fmt.Sprintf(`"gw-init-%s"`, b.ID()))
			req := &rpc.Request{JSONRPC: rpc.Version, Method: "initialize", ID: subID, Params: hostParams()}
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
		return nil, fmt.Errorf("aggregate: initialize backends: %w", err)
	}

	merged, err := mergeInitializeResults(results, a.backends)
	if err != nil {
		return rpc.NewError(hostID, errcodes.GatewayInternal, "gateway: all backends failed initialize", nil), nil
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("aggregate: marshal initialize result: %w", err)
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

func mergeInitializeResults(results []json.RawMessage, backends []backend.Backend) (map[string]any, error) {
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
	for i, b := range backends {
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
		return nil, errNoBackends
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

// ToolsList returns aggregated namespaced tools in stable order: backend order, then native name.
// When the request context carries JWT claim mcp_tools (via ingress), the list is filtered to that allow-list
// (full catalog is still cached and used for router reindex and schema compilation).
func (a *Aggregator) ToolsList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, "mcp.aggregate.tools_list")
	defer span.End()

	allowed := ingress.AllowedToolsFromContext(tctx)

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
	results := make([]listResult, len(a.backends))
	g, gctx := errgroup.WithContext(tctx)
	var mu sync.Mutex

	for i, b := range a.backends {
		i, b := i, b
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(gctx, a.listTimeout)
			defer cancel()
			subID := json.RawMessage(fmt.Sprintf(`"gw-list-%s"`, b.ID()))
			req := &rpc.Request{JSONRPC: rpc.Version, Method: "tools/list", ID: subID, Params: nil}
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
		return nil, fmt.Errorf("aggregate: tools/list backends: %w", err)
	}

	var merged []map[string]any
	for i, b := range a.backends {
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
		return nil, fmt.Errorf("aggregate: marshal tools/list: %w", err)
	}
	if a.listTTL > 0 && len(allowed) == 0 {
		a.mu.Lock()
		a.cachedList = append(json.RawMessage(nil), outFull...)
		a.cachedAt = time.Now()
		a.mu.Unlock()
	}

	if a.semantic != nil && a.semantic.Enabled() {
		ver := fmt.Sprintf("%x", sha256.Sum256(outFull))
		entries, err := router.BuildCatalogEntries(outFull, func(prefix string) (string, error) {
			b, ok := a.byPrefix[prefix]
			if !ok {
				return "", fmt.Errorf("unknown prefix %q", prefix)
			}
			return b.ID(), nil
		})
		if err != nil {
			slog.Warn("router catalog build skipped", "err", err)
		} else if err := a.semantic.Reindex(tctx, ver, entries); err != nil {
			slog.Warn("router reindex failed", "err", err)
		} else {
			a.catMu.Lock()
			a.catVer = ver
			a.catMu.Unlock()
			telemetry.SetIndexedCatalogToolCount(int64(len(entries)))
		}
	}

	toReturn := outFull
	if len(allowed) > 0 {
		filtered := filterToolsForPolicy(merged, allowed)
		filteredRaw, err := json.Marshal(map[string]any{"tools": filtered})
		if err != nil {
			return nil, fmt.Errorf("aggregate: marshal filtered tools/list: %w", err)
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

func (a *Aggregator) invalidateToolCache() {
	a.mu.Lock()
	a.cachedList = nil
	a.mu.Unlock()
	a.schemaMu.Lock()
	a.toolValidators = nil
	a.schemaMu.Unlock()
}

// ToolsCall resolves the namespaced tool to a backend, strips the prefix for upstream, and preserves the JSON-RPC id.
func (a *Aggregator) ToolsCall(ctx context.Context, hostID json.RawMessage, params json.RawMessage) (*rpc.Response, error) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, "invalid tools/call params", nil), nil
	}

	if a.semantic != nil && a.semantic.Enabled() {
		rctx, span := telemetry.StartSpan(ctx, "mcp.router.semantic")
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

	allowed := ingress.AllowedToolsFromContext(ctx)
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
		return nil, fmt.Errorf("aggregate: marshal tools/call forward params: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
	bctx, bspan := telemetry.StartSpan(callCtx, "mcp.backend.call")
	defer bspan.End()
	bspan.SetAttributes(attribute.String("backend_id", b.ID()))
	req := &rpc.Request{
		JSONRPC: rpc.Version,
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

func (a *Aggregator) semanticRoutingSignal(ctx context.Context, toolName string, args json.RawMessage) router.RoutingSignal {
	a.catMu.RLock()
	ver := a.catVer
	a.catMu.RUnlock()
	allowedList := ingress.AllowedToolsFromContext(ctx)
	return router.RoutingSignal{
		Method:         "tools/call",
		ToolName:       toolName,
		ArgumentsJSON:  args,
		IntentText:     ingress.MCPIntentFromContext(ctx),
		AllowedTools:   allowedList,
		CatalogVersion: ver,
	}
}
