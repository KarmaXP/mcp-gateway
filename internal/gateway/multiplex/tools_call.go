package multiplex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func (a *Multiplexer) ToolsCall(ctx context.Context, hostID json.RawMessage, params json.RawMessage) (*rpc.Response, error) {
	p, errResp := parseToolsCallParams(hostID, params)
	if errResp != nil {
		return errResp, nil
	}

	mode, _ := hostctx.AllowListModeFromContext(ctx)
	authorizedName := ""
	switch mode {
	case hostctx.AllowListDenyAll:
		if errResp := a.enforceHostToolAuthz(ctx, hostID, p.Name); errResp != nil {
			return errResp, nil
		}
		authorizedName = p.Name
	case hostctx.AllowListRestricted:
		if a.semantic == nil || !a.semantic.AllowAutoRename() {
			if errResp := a.enforceHostToolAuthz(ctx, hostID, p.Name); errResp != nil {
				return errResp, nil
			}
			authorizedName = p.Name
		}
	}

	if errResp := a.applySemanticToolRouting(ctx, hostID, &p); errResp != nil {
		return errResp, nil
	}

	if p.Name != authorizedName {
		if errResp := a.enforceHostToolAuthz(ctx, hostID, p.Name); errResp != nil {
			return errResp, nil
		}
	}

	argsForForward := coalesceArgs(p.Arguments)
	if errResp := a.validateToolArgsWithSpan(ctx, hostID, p.Name, argsForForward); errResp != nil {
		return errResp, nil
	}

	muxStart := time.Now()
	b, native, err := a.resolveBackendForTool(p.Name)
	if err != nil {
		telemetry.RecordInternalPhase(ctx, "tools/call", defaults.MetricInternalPhaseMux, time.Since(muxStart))
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil), nil
	}

	return a.invokeUpstreamToolsCall(ctx, hostID, b, p.Name, native, argsForForward, muxStart)
}

func (a *Multiplexer) enforceHostToolAuthz(ctx context.Context, hostID json.RawMessage, namespacedTool string) *rpc.Response {
	secStart := time.Now()
	defer func() {
		telemetry.RecordInternalPhase(ctx, "tools/call", defaults.MetricInternalPhaseSecurity, time.Since(secStart))
	}()
	actx, span := telemetry.StartSpan(ctx, telemetry.SpanSecurityAuthz)
	defer span.End()
	span.SetAttributes(attribute.String(telemetry.AttrMCPToolName, namespacedTool))

	mode, names := hostctx.AllowListModeFromContext(actx)
	switch mode {
	case hostctx.AllowListUnrestricted:
		span.SetStatus(codes.Ok, "")
		return nil
	case hostctx.AllowListDenyAll:
		span.SetStatus(codes.Error, "not in allow list")
		policy.LogAudit(actx, "deny", "not_in_allow_list", namespacedTool, hostctx.SubjectIDFromContext(actx), hostctx.PolicyVersionFromContext(actx))
		return rpc.NewError(hostID, errcodes.PermissionDenied, fmt.Sprintf("tool %q not allowed for this principal", namespacedTool), nil)
	}
	ok, err := policy.AllowedListContains(namespacedTool, hostctx.PolicyAllowListView(mode, names))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "policy evaluation failed")
		policy.LogAudit(actx, "deny", "policy_eval_failed", namespacedTool, hostctx.SubjectIDFromContext(actx), hostctx.PolicyVersionFromContext(actx))
		return rpc.NewError(hostID, errcodes.GatewayInternal, "policy evaluation failed", nil)
	}
	if !ok {
		span.SetStatus(codes.Error, "not in allow list")
		policy.LogAudit(actx, "deny", "not_in_allow_list", namespacedTool, hostctx.SubjectIDFromContext(actx), hostctx.PolicyVersionFromContext(actx))
		return rpc.NewError(hostID, errcodes.PermissionDenied, fmt.Sprintf("tool %q not allowed for this principal", namespacedTool), nil)
	}
	telemetry.RecordPolicyDecision(actx, defaults.MetricPolicyOutcomeAllow, defaults.MetricPolicyReasonAllowListMatch)
	span.SetStatus(codes.Ok, "")
	return nil
}

type toolsCallParams struct {
	Name      string
	Arguments json.RawMessage
}

func parseToolsCallParams(hostID json.RawMessage, params json.RawMessage) (toolsCallParams, *rpc.Response) {
	var p toolsCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return toolsCallParams{}, rpc.NewError(hostID, errcodes.InvalidParams, "invalid tools/call params", nil)
	}
	if strings.TrimSpace(p.Name) == "" {
		return toolsCallParams{}, rpc.NewError(hostID, errcodes.InvalidParams, "tools/call requires name", nil)
	}
	return p, nil
}

func (a *Multiplexer) applySemanticToolRouting(ctx context.Context, hostID json.RawMessage, p *toolsCallParams) *rpc.Response {
	if a.semantic == nil || !a.semantic.Enabled() {
		return nil
	}
	if mode, _ := hostctx.AllowListModeFromContext(ctx); mode == hostctx.AllowListDenyAll {
		return nil
	}
	routeStart := time.Now()
	defer func() {
		telemetry.RecordInternalPhase(ctx, "tools/call", defaults.MetricInternalPhaseRouter, time.Since(routeStart))
	}()
	rctx, span := telemetry.StartSpan(ctx, telemetry.SpanSemanticRouter)
	defer span.End()
	span.SetAttributes(attribute.String(telemetry.AttrMCPMethod, "tools/call"))
	sig := a.semanticRoutingSignal(ctx, p.Name, p.Arguments)
	resolved, dec, err := a.semantic.ResolveToolsCall(rctx, sig)
	if dec != nil {
		telemetry.RecordSemanticRouting(rctx, telemetry.SemanticRouting{
			Outcome:       string(dec.Outcome),
			FallbackLayer: dec.FallbackLayer,
			LatencyMS:     dec.LatencyMS,
		}, err)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "semantic router")
		return rpc.NewError(hostID, errcodes.ToolRoutingAmbiguous, err.Error(), nil)
	}
	span.SetStatus(codes.Ok, "")
	p.Name = resolved
	return nil
}

func (a *Multiplexer) resolveBackendForTool(namespaced string) (b backendCaller, native string, err error) {
	prefix, nativeName, err := namespace.Split(namespaced)
	if err != nil {
		return nil, "", err
	}
	up, ok := a.byPrefix[prefix]
	if !ok {
		return nil, "", fmt.Errorf("unknown tool prefix in %q", namespaced)
	}
	return up, nativeName, nil
}

type backendCaller interface {
	ID() string
	Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error)
}

func (a *Multiplexer) invokeUpstreamToolsCall(ctx context.Context, hostID json.RawMessage, b backendCaller, namespacedTool, native string, args json.RawMessage, muxStart time.Time) (*rpc.Response, error) {
	forwardParams, err := json.Marshal(map[string]any{
		"name":      native,
		"arguments": args,
	})
	if err != nil {
		return nil, fmt.Errorf("multiplex: marshal tools/call forward params: %w", err)
	}
	telemetry.RecordInternalPhase(ctx, "tools/call", defaults.MetricInternalPhaseMux, time.Since(muxStart))
	callCtx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
	release, err := a.acquireGlobalCallSlot(callCtx)
	if err != nil {
		return rpc.NewError(hostID, errcodes.GatewayInternal, "backend call failed", nil), nil
	}
	defer release()
	bctx, bspan := telemetry.StartSpan(callCtx, telemetry.SpanBackendCall)
	defer bspan.End()
	bspan.SetAttributes(
		attribute.String(telemetry.AttrMCPBackendID, b.ID()),
		attribute.String(telemetry.AttrMCPMethod, "tools/call"),
	)
	req := &rpc.Request{
		JSONRPC: rpc.JSONRPCVersion,
		Method:  "tools/call",
		ID:      hostID,
		Params:  forwardParams,
	}
	resp, err := b.Call(bctx, req)
	if err != nil {
		bspan.RecordError(err)
		bspan.SetStatus(codes.Error, "backend transport")
		return rpc.NewError(hostID, errcodes.GatewayInternal, "backend call failed", nil), nil
	}
	if resp == nil {
		bspan.SetStatus(codes.Error, "upstream empty response")
		return rpc.NewError(hostID, errcodes.GatewayInternal, "backend call failed", nil), nil
	}
	if resp.Error != nil {
		bspan.SetStatus(codes.Error, "upstream jsonrpc error")
	} else {
		bspan.SetStatus(codes.Ok, "")
	}
	if resp.Error == nil && !isToolResultError(resp.Result) {
		hostctx.RecordSuccessfulToolCall(ctx, namespacedTool)
	}
	resp.ID = hostID
	return resp, nil
}

func isToolResultError(result json.RawMessage) bool {
	if len(result) == 0 {
		return false
	}
	var envelope struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &envelope); err != nil {
		return false
	}
	return envelope.IsError
}
