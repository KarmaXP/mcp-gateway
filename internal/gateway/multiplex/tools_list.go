package multiplex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/mcpwire"
	"log/slog"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
	"github.com/KarmaXP/mcp-gateway/internal/upstream"
)

func (a *Multiplexer) ToolsList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexToolsList)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPMethod, mcpwire.MethodToolsList),
		telemetry.AttrJSONRPCID(hostID),
	)

	allowMode, allowed := hostctx.AllowListModeFromContext(tctx)
	if resp, ok := a.tryCachedToolsList(tctx, hostID, allowMode); ok {
		span.SetStatus(codes.Ok, "")
		return resp, nil
	}
	if allowMode == hostctx.AllowListDenyAll {
		return a.respondWithToolsList(tctx, hostID, nil, allowMode, allowed, nil), nil
	}

	merged, listFailures, err := a.fetchAndMergeUpstreamTools(tctx)
	if err != nil {
		span.SetStatus(codes.Error, "tools/list upstream strict")
		return rpc.NewError(hostID, errcodes.StrictAggregationFailed, "tools/list: strict aggregation: one or more upstreams failed", nil), nil
	}
	a.replaceToolSchemasFromMerged(merged, listFailures)

	outFull, err := json.Marshal(map[string]any{"tools": merged})
	if err != nil {
		span.SetStatus(codes.Error, "marshal tools/list")
		return nil, fmt.Errorf("multiplex: marshal tools/list: %w", err)
	}
	a.storeFullToolsListCache(outFull, allowMode)
	a.maybeReindexSemanticCatalog(tctx, merged, outFull)

	forClient := a.narrowToolsForIntent(tctx, merged, allowMode, allowed)
	return a.respondWithToolsList(tctx, hostID, forClient, allowMode, allowed, listFailures), nil
}

func (a *Multiplexer) narrowToolsForIntent(ctx context.Context, merged []map[string]any, allowMode hostctx.AllowListMode, allowed []string) []map[string]any {
	if a.semantic == nil || !a.semantic.FilterListActive() {
		return merged
	}
	intent := hostctx.ClientIntentFromContext(ctx)
	if intent == "" {
		return merged
	}
	sig := router.RoutingSignal{
		Method:         mcpwire.MethodToolsList,
		IntentText:     intent,
		AllowedTools:   allowed,
		AllowListAuthz: routerAllowListAuthz(allowMode),
		CatalogVersion: a.catalogVersion.load(),
	}
	rctx, sp := telemetry.StartSpan(ctx, telemetry.SpanSemanticRouter)
	defer sp.End()
	routeStart := time.Now()
	keep, useFull := a.semantic.FilterToolsForList(rctx, sig)
	telemetry.RecordInternalPhase(rctx, mcpwire.MethodToolsList, defaults.MetricInternalPhaseRouter, time.Since(routeStart))
	if useFull || len(keep) == 0 {
		return merged
	}
	return filterMergedByToolNames(merged, keep)
}

func (a *Multiplexer) respondWithToolsList(ctx context.Context, hostID json.RawMessage, merged []map[string]any, allowMode hostctx.AllowListMode, allowed []string, failures []PartialFailure) *rpc.Response {
	span := trace.SpanFromContext(ctx)
	muxStart := time.Now()
	payload, err := a.toolsListPayloadForClient(merged, allowMode, allowed, failures)
	telemetry.RecordInternalPhase(ctx, mcpwire.MethodToolsList, defaults.MetricInternalPhaseMux, time.Since(muxStart))
	if err != nil {
		span.SetStatus(codes.Error, "tools/list policy")
		return rpc.NewError(hostID, errcodes.GatewayInternal, "tools/list policy failed", nil)
	}
	span.SetStatus(codes.Ok, "")
	return rpc.NewResult(hostID, payload)
}

func (a *Multiplexer) tryCachedToolsList(ctx context.Context, hostID json.RawMessage, mode hostctx.AllowListMode) (*rpc.Response, bool) {
	if mode != hostctx.AllowListUnrestricted {
		return nil, false
	}
	if a.semantic != nil && a.semantic.FilterListActive() && hostctx.ClientIntentFromContext(ctx) != "" {
		return nil, false
	}
	cachedCopy, valid := a.listCache.load()
	if !valid {
		return nil, false
	}
	a.refreshToolSchemasFromListJSON(cachedCopy)
	return rpc.NewResult(hostID, cachedCopy), true
}

func (a *Multiplexer) fetchAndMergeUpstreamTools(ctx context.Context) ([]map[string]any, []PartialFailure, error) {
	perUpstream, failures, anyFail := a.fanoutListMethod(ctx, mcpwire.MethodToolsList, a.callUpstreamToolsList)
	if a.strictList && anyFail {
		return nil, nil, fmt.Errorf("tools/list: upstream failure")
	}
	return mergeNamespacedToolList(a.upstreams, perUpstream), failures, nil
}

func (a *Multiplexer) callUpstreamToolsList(ctx context.Context, b upstream.Client) ([]map[string]any, *PartialFailure) {
	callCtx, cancel := context.WithTimeout(ctx, a.listTimeout)
	defer cancel()
	release, err := a.acquireGlobalCallSlot(callCtx)
	if err != nil {
		slog.Warn("tools/list semaphore wait failed", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{UpstreamID: b.ID(), Reason: classifyCallFailure(err)}
	}
	defer release()
	subID := json.RawMessage(fmt.Sprintf(`"gw-list-%s"`, b.ID()))
	req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: mcpwire.MethodToolsList, ID: subID, Params: nil}
	resp, err := b.Call(callCtx, req)
	if err != nil {
		slog.Warn("tools/list backend failed", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{UpstreamID: b.ID(), Reason: classifyCallFailure(err)}
	}
	if resp == nil {
		slog.Warn("tools/list backend returned nil response", "backend_id", b.ID())
		return nil, &PartialFailure{UpstreamID: b.ID(), Reason: PartialFailureOmitted}
	}
	if resp.Error != nil {
		slog.Warn("tools/list jsonrpc error", "backend_id", b.ID(), "message", resp.Error.Message)
		return nil, &PartialFailure{UpstreamID: b.ID(), Reason: PartialFailureJSONRPC}
	}
	var body struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &body); err != nil {
		slog.Warn("tools/list decode", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{UpstreamID: b.ID(), Reason: PartialFailureOmitted}
	}
	return body.Tools, nil
}

func mergeNamespacedToolList(upstreams []upstream.Client, perUpstream [][]map[string]any) []map[string]any {
	mergedCap := 0
	for i := range upstreams {
		if i < len(perUpstream) {
			mergedCap += len(perUpstream[i])
		}
	}
	merged := make([]map[string]any, 0, mergedCap)
	seen := make(map[string]struct{}, mergedCap)
	for i, b := range upstreams {
		if i >= len(perUpstream) {
			continue
		}
		tools := perUpstream[i]
		sort.Slice(tools, func(i, j int) bool {
			n1, _ := tools[i]["name"].(string)
			n2, _ := tools[j]["name"].(string)
			return n1 < n2
		})
		for _, t := range tools {
			name, _ := t["name"].(string)
			ns, err := namespace.Join(b.Prefix(), name)
			if err != nil {
				slog.Warn("skip tool (namespace)", "backend_id", b.ID(), "tool", name, "err", err)
				continue
			}
			if _, dup := seen[ns]; dup {
				slog.Warn("skip duplicate tool name in merge", "name", ns, "backend_id", b.ID())
				continue
			}
			seen[ns] = struct{}{}
			clone := cloneMap(t)
			clone["name"] = ns
			merged = append(merged, clone)
		}
	}
	return merged
}

func (a *Multiplexer) storeFullToolsListCache(outFull []byte, mode hostctx.AllowListMode) {
	if mode != hostctx.AllowListUnrestricted {
		return
	}
	a.listCache.store(outFull)
}

func (a *Multiplexer) maybeReindexSemanticCatalog(ctx context.Context, merged []map[string]any, outFull []byte) {
	if a.semantic == nil || !a.semantic.Enabled() {
		return
	}
	ver := fmt.Sprintf("%x", sha256.Sum256(outFull))
	if a.catalogVersion.isCurrent(ver) {
		return
	}
	indexed, err := router.BuildIndexedToolsFromMerged(merged, func(prefix string) (string, error) {
		b, ok := a.byPrefix[prefix]
		if !ok {
			return "", fmt.Errorf("unknown prefix %q", prefix)
		}
		return b.ID(), nil
	})
	if err != nil {
		slog.Warn("router catalog build skipped", "err", err)
		return
	}
	refreshGen := a.catalogVersion.beginRefresh()
	if err := a.semantic.Reindex(ctx, ver, indexed); err != nil {
		slog.Warn("router reindex failed", "err", err)
		return
	}
	a.commitSemanticCatalogVersion(ctx, ver, indexed, refreshGen)
}

func (a *Multiplexer) commitSemanticCatalogVersion(ctx context.Context, ver string, indexed []router.IndexedTool, refreshGen uint64) {
	a.catalogVersion.commitIfCurrent(ver, refreshGen, func() {
		if a.semantic != nil {
			a.semantic.ApplyCatalog(ctx, ver, indexed)
		}
		telemetry.SetIndexedCatalogToolCount(int64(len(indexed)))
	})
}

func (a *Multiplexer) toolsListPayloadForClient(merged []map[string]any, mode hostctx.AllowListMode, allowed []string, failures []PartialFailure) (json.RawMessage, error) {
	var payload map[string]any
	if mode == hostctx.AllowListUnrestricted {
		payload = map[string]any{"tools": merged}
	} else {
		filtered, err := filterToolsForPolicy(merged, mode, allowed)
		if err != nil {
			return nil, fmt.Errorf("multiplex: tools/list policy: %w", err)
		}
		payload = map[string]any{"tools": filtered}
	}
	if a.reportPartialFailures && len(failures) > 0 {
		attachListExtrasPartialFailures(payload, failures)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal tools/list: %w", err)
	}
	return raw, nil
}

func filterMergedByToolNames(merged []map[string]any, keep map[string]struct{}) []map[string]any {
	if len(keep) == 0 {
		return merged
	}
	out := make([]map[string]any, 0, len(keep))
	for _, t := range merged {
		name, _ := t["name"].(string)
		if _, ok := keep[name]; ok {
			out = append(out, t)
		}
	}
	return out
}
