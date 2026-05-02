package multiplex

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/namespace"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

func (a *Multiplexer) ToolsCall(ctx context.Context, hostID json.RawMessage, params json.RawMessage) (*rpc.Response, error) {
	p, errResp := parseToolsCallParams(hostID, params)
	if errResp != nil {
		return errResp, nil
	}

	if errResp := a.applySemanticToolRouting(ctx, hostID, &p); errResp != nil {
		return errResp, nil
	}

	if err := enforceToolPolicy(hostctx.AllowedToolNamesFromContext(ctx), p.Name); err != nil {
		return rpc.NewError(hostID, errcodes.RequestRejected, err.Error(), nil), nil
	}

	b, native, err := a.resolveBackendForTool(p.Name)
	if err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil), nil
	}

	argsForForward := coalesceArgs(p.Arguments)
	if err := a.validateToolArgs(p.Name, argsForForward); err != nil {
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil), nil
	}

	return a.invokeUpstreamToolsCall(ctx, hostID, b, native, argsForForward)
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
	return p, nil
}

func (a *Multiplexer) applySemanticToolRouting(ctx context.Context, hostID json.RawMessage, p *toolsCallParams) *rpc.Response {
	if a.semantic == nil || !a.semantic.Enabled() {
		return nil
	}
	rctx, span := telemetry.StartSpan(ctx, telemetry.SpanSemanticRouter)
	sig := a.semanticRoutingSignal(ctx, p.Name, p.Arguments)
	resolved, dec, err := a.semantic.ResolveToolsCall(rctx, sig)
	telemetry.RecordSemanticRouting(rctx, dec, err)
	if err != nil {
		span.RecordError(err)
		span.End()
		return rpc.NewError(hostID, errcodes.ToolRoutingAmbiguous, err.Error(), nil)
	}
	span.End()
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

func (a *Multiplexer) invokeUpstreamToolsCall(ctx context.Context, hostID json.RawMessage, b backendCaller, native string, args json.RawMessage) (*rpc.Response, error) {
	forwardParams, err := json.Marshal(map[string]any{
		"name":      native,
		"arguments": args,
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
