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

	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

var errNoBackends = errors.New("aggregate: no backends responded to initialize")

// Aggregator merges initialize and tools/list across backends (§3.A). Order follows the backend slice (stable prefix order).
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

	semantic *router.Engine // optional §3.B

	catMu  sync.RWMutex
	catVer string // sha256 of last aggregated tools/list JSON (for optional client pinning)
}

// Option configures the aggregator.
type Option func(*Aggregator)

// WithInitTimeout sets per-backend initialize deadline (R5).
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

// WithSemanticRouter attaches the §3.B engine (nil-safe: no-op if e == nil or ModeOff).
func WithSemanticRouter(e *router.Engine) Option {
	return func(a *Aggregator) { a.semantic = e }
}

// New builds an aggregator. Backend prefixes must be unique.
func New(backends []backend.Backend, opts ...Option) (*Aggregator, error) {
	byPrefix := make(map[string]backend.Backend, len(backends))
	for _, b := range backends {
		p := b.Prefix()
		if err := namespace.ValidatePrefix(p); err != nil {
			return nil, err
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

// Initialize fans out to all backends, omits failures (R6), errors only if all fail.
func (a *Aggregator) Initialize(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	results := make([]json.RawMessage, len(a.backends))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
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
		return nil, err
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
	// Minimal initialize params acceptable to mocks/real servers.
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
		// shallow merge nested maps
		dm, dOK := dst[k].(map[string]any)
		vm, vOK := v.(map[string]any)
		if dOK && vOK {
			shallowMerge(dm, vm)
		}
	}
}

// ToolsList returns aggregated namespaced tools in stable order: backend order, then native name.
func (a *Aggregator) ToolsList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	if a.listTTL > 0 {
		a.mu.RLock()
		if len(a.cachedList) > 0 && time.Since(a.cachedAt) < a.listTTL {
			c := append(json.RawMessage(nil), a.cachedList...)
			a.mu.RUnlock()
			return rpc.NewResult(hostID, c), nil
		}
		a.mu.RUnlock()
	}

	type listResult struct {
		tools []map[string]any
	}
	results := make([]listResult, len(a.backends))
	// errgroup.WithContext cancels the derived ctx when Wait returns; Reindex must use the caller ctx.
	listCtx := ctx
	g, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex

	for i, b := range a.backends {
		i, b := i, b
		g.Go(func() error {
			callCtx, cancel := context.WithTimeout(ctx, a.listTimeout)
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
		return nil, err
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
	// Order: configured backend list (§A.7), then native tool name ascending within each backend.

	out, err := json.Marshal(map[string]any{"tools": merged})
	if err != nil {
		return nil, err
	}
	if a.listTTL > 0 {
		a.mu.Lock()
		a.cachedList = append(json.RawMessage(nil), out...)
		a.cachedAt = time.Now()
		a.mu.Unlock()
	}

	if a.semantic != nil && a.semantic.Enabled() {
		ver := fmt.Sprintf("%x", sha256.Sum256(out))
		entries, err := router.BuildCatalogEntries(out, func(prefix string) (string, error) {
			b, ok := a.byPrefix[prefix]
			if !ok {
				return "", fmt.Errorf("unknown prefix %q", prefix)
			}
			return b.ID(), nil
		})
		if err != nil {
			slog.Warn("router catalog build skipped", "err", err)
		} else if err := a.semantic.Reindex(listCtx, ver, entries); err != nil {
			slog.Warn("router reindex failed", "err", err)
		} else {
			a.catMu.Lock()
			a.catVer = ver
			a.catMu.Unlock()
		}
	}

	return rpc.NewResult(hostID, out), nil
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
}

// ToolsCall resolves prefix, strips name (R2), forwards with same JSON-RPC id (R3).
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
		sig := router.RoutingSignal{
			Method:        "tools/call",
			ToolName:      p.Name,
			ArgumentsJSON: p.Arguments,
		}
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

	prefix, native, err := namespace.Split(p.Name)
	if err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil), nil
	}
	b, ok := a.byPrefix[prefix]
	if !ok {
		return rpc.NewError(hostID, errcodes.InvalidParams, fmt.Sprintf("unknown tool prefix in %q", p.Name), nil), nil
	}
	forwardParams, err := json.Marshal(map[string]any{
		"name":      native,
		"arguments": coalesceArgs(p.Arguments),
	})
	if err != nil {
		return nil, fmt.Errorf("aggregate: marshal tools/call forward params: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
	bctx, bspan := telemetry.StartSpan(callCtx, "mcp.backend.tools_call")
	defer bspan.End()
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
