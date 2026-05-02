package multiplex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func (a *Multiplexer) ToolsList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexToolsList)
	defer span.End()

	allowed := hostctx.AllowedToolNamesFromContext(tctx)
	if resp, ok := a.tryCachedToolsList(tctx, hostID, allowed); ok {
		return resp, nil
	}

	merged := a.fetchAndMergeUpstreamTools(tctx)

	a.replaceToolSchemasFromMerged(merged)

	outFull, err := json.Marshal(map[string]any{"tools": merged})
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal tools/list: %w", err)
	}

	a.storeFullToolsListCache(outFull, allowed)
	a.maybeReindexSemanticCatalog(tctx, merged, outFull)

	mergedForList := merged
	if a.semantic != nil && a.semantic.FilterListActive() {
		if intent := hostctx.ClientIntentFromContext(tctx); intent != "" {
			a.catMu.RLock()
			ver := a.catVer
			a.catMu.RUnlock()
			sig := router.RoutingSignal{
				Method:         "tools/list",
				IntentText:     intent,
				AllowedTools:   allowed,
				CatalogVersion: ver,
			}
			rctx, sp := telemetry.StartSpan(tctx, telemetry.SpanSemanticRouter)
			routeStart := time.Now()
			keep, useFull := a.semantic.FilterToolsForList(rctx, sig)
			telemetry.RecordInternalPhase(rctx, "tools/list", defaults.MetricInternalPhaseRouter, time.Since(routeStart))
			sp.End()
			if !useFull && len(keep) > 0 {
				mergedForList = filterMergedByToolNames(merged, keep)
			}
		}
	}

	muxStart := time.Now()
	toReturn, err := a.toolsListPayloadForClient(mergedForList, allowed)
	telemetry.RecordInternalPhase(tctx, "tools/list", defaults.MetricInternalPhaseMux, time.Since(muxStart))
	if err != nil {
		return rpc.NewError(hostID, errcodes.GatewayInternal, "tools/list policy failed", nil), nil
	}
	return rpc.NewResult(hostID, toReturn), nil
}

func (a *Multiplexer) tryCachedToolsList(ctx context.Context, hostID json.RawMessage, allowed []string) (*rpc.Response, bool) {
	if a.listTTL <= 0 || len(allowed) != 0 {
		return nil, false
	}
	if a.semantic != nil && a.semantic.FilterListActive() && hostctx.ClientIntentFromContext(ctx) != "" {
		return nil, false
	}
	a.mu.RLock()
	valid := len(a.cachedList) > 0 && time.Since(a.cachedAt) < a.listTTL
	var cachedCopy json.RawMessage
	if valid {
		cachedCopy = append(json.RawMessage(nil), a.cachedList...)
	}
	a.mu.RUnlock()
	if !valid {
		return nil, false
	}
	a.refreshToolSchemasFromListJSON(cachedCopy)
	return rpc.NewResult(hostID, cachedCopy), true
}

func (a *Multiplexer) fetchAndMergeUpstreamTools(ctx context.Context) []map[string]any {
	perUpstream := a.listToolsFromEachUpstream(ctx)
	return mergeNamespacedToolList(a.upstreams, perUpstream)
}

func (a *Multiplexer) listToolsFromEachUpstream(ctx context.Context) [][]map[string]any {
	n := len(a.upstreams)
	results := make([][]map[string]any, n)
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex

	for i, b := range a.upstreams {
		i, b := i, b
		g.Go(func() error {
			tools := a.callUpstreamToolsList(gctx, b)
			mu.Lock()
			results[i] = tools
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		slog.Warn("tools/list upstream group", "err", err)
	}
	return results
}

func (a *Multiplexer) callUpstreamToolsList(ctx context.Context, b backend.Upstream) []map[string]any {
	callCtx, cancel := context.WithTimeout(ctx, a.listTimeout)
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
	return body.Tools
}

func mergeNamespacedToolList(upstreams []backend.Upstream, perUpstream [][]map[string]any) []map[string]any {
	mergedCap := 0
	for i := range upstreams {
		if i < len(perUpstream) {
			mergedCap += len(perUpstream[i])
		}
	}
	merged := make([]map[string]any, 0, mergedCap)
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
			clone := cloneMap(t)
			clone["name"] = ns
			merged = append(merged, clone)
		}
	}
	return merged
}

func (a *Multiplexer) storeFullToolsListCache(outFull []byte, allowed []string) {
	if a.listTTL <= 0 || len(allowed) != 0 {
		return
	}
	a.mu.Lock()
	a.cachedList = append(json.RawMessage(nil), outFull...)
	a.cachedAt = time.Now()
	a.mu.Unlock()
}

func (a *Multiplexer) maybeReindexSemanticCatalog(ctx context.Context, merged []map[string]any, outFull []byte) {
	if a.semantic == nil || !a.semantic.Enabled() {
		return
	}
	ver := fmt.Sprintf("%x", sha256.Sum256(outFull))
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
	if err := a.semantic.Reindex(ctx, ver, indexed); err != nil {
		slog.Warn("router reindex failed", "err", err)
		return
	}
	a.catMu.Lock()
	a.catVer = ver
	a.catMu.Unlock()
	telemetry.SetIndexedCatalogToolCount(int64(len(indexed)))
}

func (a *Multiplexer) toolsListPayloadForClient(merged []map[string]any, allowed []string) (json.RawMessage, error) {
	if len(allowed) == 0 {
		raw, err := json.Marshal(map[string]any{"tools": merged})
		if err != nil {
			return nil, fmt.Errorf("multiplex: marshal tools/list: %w", err)
		}
		return raw, nil
	}
	filtered, err := filterToolsForPolicy(merged, allowed)
	if err != nil {
		return nil, fmt.Errorf("multiplex: tools/list policy: %w", err)
	}
	filteredRaw, err := json.Marshal(map[string]any{"tools": filtered})
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal filtered tools/list: %w", err)
	}
	return filteredRaw, nil
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
