// Host JSON-RPC merged across MCP upstreams; optional semantic router and argument validation.
package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

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

const (
	jsonNullLiteral = "null"
	defaultToolsListChangedDelay = 200 * time.Millisecond
)

var emptyToolArguments = json.RawMessage(`{}`)

type Multiplexer struct {
	upstreams []backend.Upstream
	byPrefix  map[string]backend.Upstream

	initTimeout time.Duration
	listTimeout time.Duration
	callTimeout time.Duration

	listCache listCache

	semantic *router.SemanticRouter

	policyHolder *policy.Holder
	auditor      *policy.Auditor
	argLimits    validate.Limits

	catalogVersion catalogVersion

	schemaRegistry schemaRegistry

	strictInit            bool
	strictList            bool
	reportPartialFailures bool
	globalCallSemaphore   *semaphore.Weighted

	lifecycleCtx context.Context

	initializeOnce initializeOnce

	listChangedDebouncer listChangedDebouncer
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
	return func(a *Multiplexer) { a.listCache.ttl = d }
}

func WithSemanticRouter(sr *router.SemanticRouter) Option {
	return func(a *Multiplexer) { a.semantic = sr }
}

func WithPolicyHolder(h *policy.Holder) Option {
	return func(a *Multiplexer) { a.policyHolder = h }
}

func WithAuditor(a *policy.Auditor) Option {
	return func(m *Multiplexer) { m.auditor = a }
}

func WithPolicyEngine(p *policy.Engine) Option {
	return func(a *Multiplexer) {
		if p == nil {
			a.policyHolder = nil
			return
		}
		a.policyHolder = policy.NewHolder(p)
	}
}

func WithArgumentValidateLimits(l validate.Limits) Option {
	return func(a *Multiplexer) { a.argLimits = l }
}

// WithAggregationStrict enables fail-closed aggregation (initialize and/or list RPCs).
func WithAggregationStrict(strictInitialize, strictList bool) Option {
	return func(a *Multiplexer) {
		a.strictInit = strictInitialize
		a.strictList = strictList
	}
}

func WithReportPartialFailures(report bool) Option {
	return func(a *Multiplexer) { a.reportPartialFailures = report }
}

func WithGlobalMaxInFlight(maxInFlight int) Option {
	return func(a *Multiplexer) {
		if maxInFlight <= 0 {
			a.globalCallSemaphore = nil
			return
		}
		a.globalCallSemaphore = semaphore.NewWeighted(int64(maxInFlight))
	}
}

// WithLifecycleContext bounds background work (e.g. catalog refresh on tools/list_changed).

// WithToolsListChangedDebounce coalesces upstream list_changed refresh work (0 disables debounce).
func WithToolsListChangedDebounce(d time.Duration) Option {
	return func(a *Multiplexer) { a.listChangedDebouncer.delay = d }
}

func New(lifecycle context.Context, upstreams []backend.Upstream, opts ...Option) (*Multiplexer, error) {
	if lifecycle == nil {
		lifecycle = context.Background()
	}
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
		lifecycleCtx:         lifecycle,
		upstreams:            append([]backend.Upstream(nil), upstreams...),
		byPrefix:             byPrefix,
		initTimeout:          defaults.MultiplexInitTimeout,
		listTimeout:          defaults.MultiplexListTimeout,
		callTimeout:          defaults.MultiplexCallTimeout,
		listCache:            listCache{ttl: defaults.MultiplexListCacheTTL},
		listChangedDebouncer: listChangedDebouncer{delay: defaultToolsListChangedDelay},
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

func (a *Multiplexer) initializeUpstream(ctx context.Context, index int, b backend.Upstream, outcome *initializeOutcome) {
	callCtx, cancel := context.WithTimeout(ctx, a.initTimeout)
	defer cancel()
	release, err := a.acquireGlobalCallSlot(callCtx)
	if err != nil {
		slog.Warn("initialize backend semaphore wait failed", "backend_id", b.ID(), "err", err)
		outcome.recordFailure(b.ID(), classifyCallFailure(err))
		return
	}
	defer release()

	subID := json.RawMessage(fmt.Sprintf(`"gw-init-%s"`, b.ID()))
	req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "initialize", ID: subID, Params: upstreamInitParams(ctx)}
	resp, err := b.Call(callCtx, req)
	switch {
	case err != nil:
		slog.Warn("initialize backend failed", "backend_id", b.ID(), "err", err)
		outcome.recordFailure(b.ID(), classifyCallFailure(err))
	case resp == nil:
		slog.Warn("initialize backend returned nil response", "backend_id", b.ID())
		outcome.recordFailure(b.ID(), PartialFailureOmitted)
	case resp.Error != nil:
		slog.Warn("initialize backend jsonrpc error", "backend_id", b.ID(), "code", resp.Error.Code, "message", resp.Error.Message)
		outcome.recordFailure(b.ID(), PartialFailureJSONRPC)
	default:
		outcome.recordResult(index, resp.Result)
	}
}

func (a *Multiplexer) initializeFailures(outcome *initializeOutcome, mergeFailures []PartialFailure) []PartialFailure {
	return append(append([]PartialFailure(nil), outcome.failures...), mergeFailures...)
}

func (a *Multiplexer) initializeFailuresOrOmitted(outcome *initializeOutcome, mergeFailures []PartialFailure) []PartialFailure {
	all := a.initializeFailures(outcome, mergeFailures)
	if len(all) > 0 {
		return all
	}
	for i, b := range a.upstreams {
		if len(outcome.results[i]) == 0 {
			all = append(all, PartialFailure{BackendID: b.ID(), Reason: PartialFailureOmitted})
		}
	}
	return all
}

func (a *Multiplexer) Initialize(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexInit)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPMethod, "initialize"),
		telemetry.AttrJSONRPCID(hostID),
	)

	if cached, ok := a.initializeOnce.load(); ok {
		span.SetStatus(codes.Ok, "")
		return rpc.NewResult(hostID, cached), nil
	}

	outcome := newInitializeOutcome(a.strictInit, len(a.upstreams))

	g, ctx := errgroup.WithContext(tctx)
	for i, b := range a.upstreams {
		i, b := i, b
		g.Go(func() error {
			a.initializeUpstream(ctx, i, b, outcome)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		span.SetStatus(codes.Error, "initialize upstream group")
		return nil, fmt.Errorf("multiplex: initialize upstreams: %w", err)
	}

	if a.strictInit && outcome.strictFailed {
		span.SetStatus(codes.Error, "strict initialize aggregation")
		return rpc.NewError(hostID, errcodes.StrictAggregationFailed, "gateway: strict initialize: one or more upstreams failed", nil), nil
	}

	merged, mergeFailures, err := mergeInitializeResults(outcome.results, a.upstreams)
	if err != nil {
		span.SetStatus(codes.Error, "all upstreams failed initialize")
		if a.reportPartialFailures {
			allFailures := a.initializeFailuresOrOmitted(outcome, mergeFailures)
			data, merr := json.Marshal(map[string]any{"partial_failures": partialFailuresToMaps(allFailures)})
			if merr != nil {
				return nil, fmt.Errorf("multiplex: marshal initialize partial failures: %w", merr)
			}
			return rpc.NewError(hostID, errcodes.GatewayInternal, "gateway: all upstreams failed initialize", data), nil
		}
		return rpc.NewError(hostID, errcodes.GatewayInternal, "gateway: all upstreams failed initialize", nil), nil
	}
	if a.reportPartialFailures {
		if allFailures := a.initializeFailures(outcome, mergeFailures); len(allFailures) > 0 {
			attachInitPartialFailures(merged, allFailures)
		}
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		span.SetStatus(codes.Error, "marshal initialize")
		return nil, fmt.Errorf("multiplex: marshal initialize result: %w", err)
	}
	everyUpstreamInitialized := len(outcome.failures) == 0
	if everyUpstreamInitialized {
		a.initializeOnce.store(raw)
	}
	a.invalidateListCache()
	span.SetStatus(codes.Ok, "")
	return rpc.NewResult(hostID, raw), nil
}

type hostInitParamsKey struct{}

func WithHostInitializeParams(ctx context.Context, params json.RawMessage) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(params) == 0 || string(params) == jsonNullLiteral {
		return ctx
	}
	return context.WithValue(ctx, hostInitParamsKey{}, append(json.RawMessage(nil), params...))
}

func hostInitializeParamsFromContext(ctx context.Context) json.RawMessage {
	if ctx == nil {
		return nil
	}
	raw, _ := ctx.Value(hostInitParamsKey{}).(json.RawMessage)
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func upstreamInitParams(ctx context.Context) json.RawMessage {
	host := hostInitializeParamsFromContext(ctx)
	if len(host) == 0 {
		return hostParams()
	}
	var hostMap, baseMap map[string]any
	if json.Unmarshal(host, &hostMap) != nil {
		return hostParams()
	}
	if json.Unmarshal(hostParams(), &baseMap) != nil {
		return host
	}
	out, err := json.Marshal(mergeInitParamMaps(baseMap, hostMap))
	if err != nil {
		return hostParams()
	}
	return out
}

func mergeInitParamMaps(base, host map[string]any) map[string]any {
	out := cloneMap(base)
	for k, v := range host {
		if k == "capabilities" {
			continue
		}
		if k == "clientInfo" {
			if bm, bOK := out[k].(map[string]any); bOK {
				if hm, hOK := v.(map[string]any); hOK {
					out[k] = mergeInitParamMaps(bm, hm)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
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

func mergeInitializeResults(results []json.RawMessage, upstreams []backend.Upstream) (map[string]any, []PartialFailure, error) {
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

	var mergeFailures []PartialFailure
	anyOK := false
	for i, b := range upstreams {
		if len(results[i]) == 0 {
			continue
		}
		var one map[string]any
		if err := json.Unmarshal(results[i], &one); err != nil {
			slog.Warn("initialize merge: skip backend", "backend_id", b.ID(), "err", err)
			mergeFailures = append(mergeFailures, PartialFailure{BackendID: b.ID(), Reason: PartialFailureOmitted})
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
		return nil, nil, errNoUpstreamsResponded
	}
	merged["serverInfo"].(map[string]any)["extras"].(map[string]any)["backends"] = backendsList
	return merged, mergeFailures, nil
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

// InvalidateToolCache drops the cached tools/list payload. Compiled schemas survive, so
// argument validation keeps working until the next successful refresh replaces them.
func (a *Multiplexer) InvalidateToolCache() {
	a.invalidateListCache()
}

func (a *Multiplexer) invalidateListCache() {
	a.listCache.invalidate()
}

func coalesceArgs(a json.RawMessage) json.RawMessage {
	if len(a) == 0 || string(a) == jsonNullLiteral {
		return emptyToolArguments
	}
	return a
}

func (a *Multiplexer) semanticRoutingSignal(ctx context.Context, toolName string, args json.RawMessage) router.RoutingSignal {
	ver := a.catalogVersion.load()
	allowMode, allowedList := hostctx.AllowListModeFromContext(ctx)
	return router.RoutingSignal{
		SessionID:       hostctx.MCPSessionIDFromContext(ctx),
		Method:          "tools/call",
		ToolName:        toolName,
		ArgumentsJSON:   args,
		IntentText:      hostctx.ClientIntentFromContext(ctx),
		AllowedTools:    allowedList,
		AllowListAuthz:  routerAllowListAuthz(allowMode),
		CatalogVersion:  ver,
		RecentToolNames: hostctx.RecentToolNamesFromContext(ctx),
	}
}

func routerAllowListAuthz(mode hostctx.AllowListMode) router.AllowListAuthz {
	switch mode {
	case hostctx.AllowListUnrestricted:
		return router.AllowListAuthzUnrestricted
	case hostctx.AllowListDenyAll:
		return router.AllowListAuthzDenyAll
	case hostctx.AllowListRestricted:
		return router.AllowListAuthzRestricted
	}
	// A mode this function does not know denies, so adding one cannot widen access.
	return router.AllowListAuthzDenyAll
}
