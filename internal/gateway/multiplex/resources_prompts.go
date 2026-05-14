package multiplex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/KarmaXP/mcp-gateway/internal/backend"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func (a *Multiplexer) ResourcesList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexResourcesList)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPMethod, "resources/list"),
		telemetry.AttrJSONRPCID(hostID),
	)
	per, failures, anyFail := a.listResourcesFromEachUpstream(tctx)
	if a.strictList && anyFail {
		span.SetStatus(codes.Error, "strict resources/list")
		return rpc.NewError(hostID, errcodes.StrictAggregationFailed, "resources/list: strict aggregation: one or more upstreams failed", nil), nil
	}
	merged := mergeNamespacedResources(a.upstreams, per)
	payload := map[string]any{"resources": merged}
	if a.reportPartialFailures && len(failures) > 0 {
		attachListExtrasPartialFailures(payload, failures)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		span.SetStatus(codes.Error, "marshal resources/list")
		return nil, fmt.Errorf("multiplex: marshal resources/list: %w", err)
	}
	span.SetStatus(codes.Ok, "")
	return rpc.NewResult(hostID, raw), nil
}

func (a *Multiplexer) ResourcesRead(ctx context.Context, hostID json.RawMessage, params json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexResourcesRead)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPMethod, "resources/read"),
		telemetry.AttrJSONRPCID(hostID),
	)
	namespacedURI, errResp := parseResourcesReadURI(hostID, params)
	if errResp != nil {
		return errResp, nil
	}
	prefix, nativeURI, err := namespace.SplitOpaque(namespacedURI)
	if err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, fmt.Sprintf("invalid resource uri: %v", err), nil), nil
	}
	b, ok := a.byPrefix[prefix]
	if !ok {
		return rpc.NewError(hostID, errcodes.InvalidParams, fmt.Sprintf("unknown resource prefix in %q", namespacedURI), nil), nil
	}
	forward, err := json.Marshal(map[string]any{"uri": nativeURI})
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal resources/read: %w", err)
	}
	return a.invokeUpstreamGeneric(tctx, hostID, b, "resources/read", forward, span)
}

func (a *Multiplexer) PromptsList(ctx context.Context, hostID json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexPromptsList)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPMethod, "prompts/list"),
		telemetry.AttrJSONRPCID(hostID),
	)
	per, failures, anyFail := a.listPromptsFromEachUpstream(tctx)
	if a.strictList && anyFail {
		span.SetStatus(codes.Error, "strict prompts/list")
		return rpc.NewError(hostID, errcodes.StrictAggregationFailed, "prompts/list: strict aggregation: one or more upstreams failed", nil), nil
	}
	merged := mergeNamespacedPrompts(a.upstreams, per)
	payload := map[string]any{"prompts": merged}
	if a.reportPartialFailures && len(failures) > 0 {
		attachListExtrasPartialFailures(payload, failures)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		span.SetStatus(codes.Error, "marshal prompts/list")
		return nil, fmt.Errorf("multiplex: marshal prompts/list: %w", err)
	}
	span.SetStatus(codes.Ok, "")
	return rpc.NewResult(hostID, raw), nil
}

func (a *Multiplexer) PromptsGet(ctx context.Context, hostID json.RawMessage, params json.RawMessage) (*rpc.Response, error) {
	tctx, span := telemetry.StartSpan(ctx, telemetry.SpanMultiplexPromptsGet)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPMethod, "prompts/get"),
		telemetry.AttrJSONRPCID(hostID),
	)
	namespacedName, args, errResp := parsePromptsGetParams(hostID, params)
	if errResp != nil {
		return errResp, nil
	}
	prefix, native, err := namespace.Split(namespacedName)
	if err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, fmt.Sprintf("invalid prompt name: %v", err), nil), nil
	}
	b, ok := a.byPrefix[prefix]
	if !ok {
		return rpc.NewError(hostID, errcodes.InvalidParams, fmt.Sprintf("unknown prompt prefix in %q", namespacedName), nil), nil
	}
	forwardObj := map[string]any{"name": native}
	if len(args) > 0 && string(args) != jsonNullLiteral {
		forwardObj["arguments"] = args
	}
	forward, err := json.Marshal(forwardObj)
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal prompts/get: %w", err)
	}
	return a.invokeUpstreamGeneric(tctx, hostID, b, "prompts/get", forward, span)
}

func (a *Multiplexer) invokeUpstreamGeneric(ctx context.Context, hostID json.RawMessage, b backendCaller, method string, params json.RawMessage, muxSpan trace.Span) (*rpc.Response, error) {
	callCtx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
	release, err := a.acquireGlobalCallSlot(callCtx)
	if err != nil {
		muxSpan.SetStatus(codes.Error, "upstream semaphore wait")
		return rpc.NewError(hostID, errcodes.GatewayInternal, "backend call failed", nil), nil
	}
	defer release()
	bctx, bspan := telemetry.StartSpan(callCtx, telemetry.SpanBackendCall)
	defer bspan.End()
	bspan.SetAttributes(
		attribute.String(telemetry.AttrMCPBackendID, b.ID()),
		attribute.String(telemetry.AttrMCPMethod, method),
	)
	req := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  method,
		ID:      hostID,
		Params:  params,
	}
	resp, err := b.Call(bctx, req)
	if err != nil {
		bspan.RecordError(err)
		bspan.SetStatus(codes.Error, "backend transport")
		muxSpan.SetStatus(codes.Error, "upstream transport")
		return rpc.NewError(hostID, errcodes.GatewayInternal, "backend call failed", nil), nil
	}
	if resp != nil && resp.Error != nil {
		bspan.SetStatus(codes.Error, "upstream jsonrpc error")
	} else {
		bspan.SetStatus(codes.Ok, "")
	}
	muxSpan.SetStatus(codes.Ok, "")
	return resp, nil
}

func parseResourcesReadURI(hostID, params json.RawMessage) (string, *rpc.Response) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
		return "", rpc.NewError(hostID, errcodes.InvalidParams, "resources/read requires uri", nil)
	}
	return p.URI, nil
}

func parsePromptsGetParams(hostID, params json.RawMessage) (namespacedName string, arguments json.RawMessage, errResp *rpc.Response) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return "", nil, rpc.NewError(hostID, errcodes.InvalidParams, "prompts/get requires name", nil)
	}
	return p.Name, p.Arguments, nil
}

func (a *Multiplexer) listResourcesFromEachUpstream(ctx context.Context) ([][]map[string]any, []PartialFailure, bool) {
	return a.fanoutListMethod(ctx, "resources/list", a.callUpstreamResourcesList)
}

func (a *Multiplexer) callUpstreamResourcesList(ctx context.Context, b backend.Upstream) ([]map[string]any, *PartialFailure) {
	callCtx, cancel := context.WithTimeout(ctx, a.listTimeout)
	defer cancel()
	release, err := a.acquireGlobalCallSlot(callCtx)
	if err != nil {
		slog.Warn("resources/list semaphore wait failed", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: classifyCallFailure(err)}
	}
	defer release()
	subID := json.RawMessage(fmt.Sprintf(`"gw-reslist-%s"`, b.ID()))
	req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "resources/list", ID: subID, Params: nil}
	resp, err := b.Call(callCtx, req)
	if err != nil {
		slog.Warn("resources/list backend failed", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: classifyCallFailure(err)}
	}
	if resp.Error != nil {
		if resp.Error.Code == errcodes.MethodNotFound {
			return []map[string]any{}, nil
		}
		slog.Warn("resources/list jsonrpc error", "backend_id", b.ID(), "message", resp.Error.Message)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: PartialFailureJSONRPC}
	}
	var body struct {
		Resources []map[string]any `json:"resources"`
	}
	if err := json.Unmarshal(resp.Result, &body); err != nil {
		slog.Warn("resources/list decode", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: PartialFailureOmitted}
	}
	return body.Resources, nil
}

func (a *Multiplexer) listPromptsFromEachUpstream(ctx context.Context) ([][]map[string]any, []PartialFailure, bool) {
	return a.fanoutListMethod(ctx, "prompts/list", a.callUpstreamPromptsList)
}

func (a *Multiplexer) callUpstreamPromptsList(ctx context.Context, b backend.Upstream) ([]map[string]any, *PartialFailure) {
	callCtx, cancel := context.WithTimeout(ctx, a.listTimeout)
	defer cancel()
	release, err := a.acquireGlobalCallSlot(callCtx)
	if err != nil {
		slog.Warn("prompts/list semaphore wait failed", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: classifyCallFailure(err)}
	}
	defer release()
	subID := json.RawMessage(fmt.Sprintf(`"gw-prlist-%s"`, b.ID()))
	req := &rpc.Request{JSONRPC: rpc.JSONRPCVersion, Method: "prompts/list", ID: subID, Params: nil}
	resp, err := b.Call(callCtx, req)
	if err != nil {
		slog.Warn("prompts/list backend failed", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: classifyCallFailure(err)}
	}
	if resp.Error != nil {
		if resp.Error.Code == errcodes.MethodNotFound {
			return []map[string]any{}, nil
		}
		slog.Warn("prompts/list jsonrpc error", "backend_id", b.ID(), "message", resp.Error.Message)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: PartialFailureJSONRPC}
	}
	var body struct {
		Prompts []map[string]any `json:"prompts"`
	}
	if err := json.Unmarshal(resp.Result, &body); err != nil {
		slog.Warn("prompts/list decode", "backend_id", b.ID(), "err", err)
		return nil, &PartialFailure{BackendID: b.ID(), Reason: PartialFailureOmitted}
	}
	return body.Prompts, nil
}

type listFetchFunc func(context.Context, backend.Upstream) ([]map[string]any, *PartialFailure)

func (a *Multiplexer) fanoutListMethod(ctx context.Context, method string, fetch listFetchFunc) ([][]map[string]any, []PartialFailure, bool) {
	n := len(a.upstreams)
	results := make([][]map[string]any, n)
	var failures []PartialFailure
	var anyFail bool
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	for i, b := range a.upstreams {
		i, b := i, b
		g.Go(func() error {
			items, fail := fetch(gctx, b)
			mu.Lock()
			if fail != nil {
				anyFail = true
				failures = append(failures, *fail)
			}
			results[i] = items
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		slog.Warn("list fanout", "method", method, "err", err)
	}
	return results, failures, anyFail
}

func mergeNamespacedResources(upstreams []backend.Upstream, perUpstream [][]map[string]any) []map[string]any {
	capacity := 0
	for i := range upstreams {
		if i < len(perUpstream) && len(perUpstream[i]) > 0 {
			capacity += len(perUpstream[i])
		}
	}
	out := make([]map[string]any, 0, capacity)
	for i, b := range upstreams {
		if i >= len(perUpstream) {
			continue
		}
		rows := perUpstream[i]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			u1, _ := rows[i]["uri"].(string)
			u2, _ := rows[j]["uri"].(string)
			return u1 < u2
		})
		for _, r := range rows {
			uri, _ := r["uri"].(string)
			if uri == "" {
				continue
			}
			nsURI, err := namespace.JoinOpaque(b.Prefix(), uri)
			if err != nil {
				slog.Warn("skip resource (namespace)", "backend_id", b.ID(), "uri", uri, "err", err)
				continue
			}
			clone := cloneMap(r)
			clone["uri"] = nsURI
			out = append(out, clone)
		}
	}
	return out
}

func mergeNamespacedPrompts(upstreams []backend.Upstream, perUpstream [][]map[string]any) []map[string]any {
	capacity := 0
	for i := range upstreams {
		if i < len(perUpstream) {
			capacity += len(perUpstream[i])
		}
	}
	out := make([]map[string]any, 0, capacity)
	for i, b := range upstreams {
		if i >= len(perUpstream) {
			continue
		}
		rows := perUpstream[i]
		if len(rows) == 0 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool {
			n1, _ := rows[i]["name"].(string)
			n2, _ := rows[j]["name"].(string)
			return n1 < n2
		})
		for _, p := range rows {
			name, _ := p["name"].(string)
			if name == "" {
				continue
			}
			ns, err := namespace.Join(b.Prefix(), name)
			if err != nil {
				slog.Warn("skip prompt (namespace)", "backend_id", b.ID(), "name", name, "err", err)
				continue
			}
			clone := cloneMap(p)
			clone["name"] = ns
			out = append(out, clone)
		}
	}
	return out
}
