package multiplex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/hostctx"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
	"github.com/KarmaXP/mcp-gateway/internal/validate"
)

func filterToolsForPolicy(merged []map[string]any, mode hostctx.AllowListMode, allowed []string) ([]map[string]any, error) {
	switch mode {
	case hostctx.AllowListUnrestricted:
		return merged, nil
	case hostctx.AllowListDenyAll:
		return []map[string]any{}, nil
	}
	policyList := hostctx.PolicyAllowListView(mode, allowed)
	out := make([]map[string]any, 0, len(merged))
	for _, t := range merged {
		name, _ := t["name"].(string)
		ok, err := policy.AllowedListContains(name, policyList)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func toolSchemaURL(namespacedName string) string {
	return "https://mcp-gateway.local/tool/" + url.PathEscape(namespacedName)
}

func compileToolValidator(namespacedName string, schemaJSON json.RawMessage) (*jsonschema.Schema, error) {
	var doc any
	if err := json.Unmarshal(schemaJSON, &doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	loc := toolSchemaURL(namespacedName)
	if err := c.AddResource(loc, doc); err != nil {
		return nil, err
	}
	return c.Compile(loc)
}

func (a *Multiplexer) replaceToolSchemasFromMerged(merged []map[string]any) {
	out := make(map[string]*jsonschema.Schema)
	var pol *policy.Engine
	if a.policyHolder != nil {
		pol = a.policyHolder.Load()
	}
	for _, t := range merged {
		name, _ := t["name"].(string)
		if name == "" {
			continue
		}
		sch, ok := t["inputSchema"]
		if !ok || sch == nil {
			continue
		}
		if pol != nil && pol.HardenSchemas() && pol.RequiresStrictSchema(name) {
			sch = hardenObjectSchemasForValidation(sch)
		}
		raw, err := json.Marshal(sch)
		if err != nil || len(raw) == 0 || string(raw) == jsonNullLiteral {
			continue
		}
		v, err := compileToolValidator(name, raw)
		if err != nil {
			slog.Warn("tool inputSchema compile skipped", "tool", name, "err", err)
			continue
		}
		out[name] = v
	}
	a.schemaMu.Lock()
	a.toolValidators = out
	a.schemaMu.Unlock()
}

func hardenObjectSchemasForValidation(v any) any {
	cp := cloneJSONLikeValue(v)
	hardenObjectSchemas(cp)
	return cp
}

func cloneJSONLikeValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = cloneJSONLikeValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneJSONLikeValue(x[i])
		}
		return out
	default:
		return v
	}
}

var schemaCombinatorKeys = []string{"allOf", "anyOf", "oneOf", "not", "if", "then", "else", "items", "prefixItems", "contains", "propertyNames"}

func hardenObjectSchemas(v any) { hardenSchemaNode(v) }

func hardenSchemaNode(v any) {
	switch x := v.(type) {
	case map[string]any:
		hardenObjectSchemaMap(x)
	case []any:
		for _, item := range x {
			hardenSchemaNode(item)
		}
	}
}

func hardenObjectSchemaMap(doc map[string]any) {
	if isObjectSchema(doc) {
		doc["additionalProperties"] = false
	}
	for _, key := range schemaCombinatorKeys {
		if vv, ok := doc[key]; ok {
			hardenSchemaNode(vv)
		}
	}
	if props, ok := doc["properties"].(map[string]any); ok {
		for _, vv := range props {
			hardenSchemaNode(vv)
		}
	}
	if pp, ok := doc["patternProperties"].(map[string]any); ok {
		for _, vv := range pp {
			hardenSchemaNode(vv)
		}
	}
	if ap, ok := doc["additionalProperties"]; ok {
		if apMap, ok := ap.(map[string]any); ok {
			hardenSchemaNode(apMap)
		}
	}
	if defs, ok := doc[""].(map[string]any); ok {
		for _, vv := range defs {
			hardenSchemaNode(vv)
		}
	}
	if defs, ok := doc["definitions"].(map[string]any); ok {
		for _, vv := range defs {
			hardenSchemaNode(vv)
		}
	}
}

func isObjectSchema(doc map[string]any) bool {
	if _, ok := doc["properties"]; ok {
		return true
	}
	t, ok := doc["type"]
	if !ok {
		return false
	}
	switch tt := t.(type) {
	case string:
		return tt == "object"
	case []any:
		for _, entry := range tt {
			if s, ok := entry.(string); ok && s == "object" {
				return true
			}
		}
	}
	return false
}

func (a *Multiplexer) refreshToolSchemasFromListJSON(raw json.RawMessage) {
	tools, err := parseToolsArrayFromListJSON(raw)
	if err != nil {
		return
	}
	a.replaceToolSchemasFromMerged(tools)
}

func parseToolsArrayFromListJSON(raw json.RawMessage) ([]map[string]any, error) {
	var body struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body.Tools, nil
}

func (a *Multiplexer) validateToolArgsWithSpan(ctx context.Context, hostID json.RawMessage, namespacedTool string, argsJSON json.RawMessage) *rpc.Response {
	_, span := telemetry.StartSpan(ctx, telemetry.SpanValidateJSONSchema)
	defer span.End()
	span.SetAttributes(
		attribute.String(telemetry.AttrMCPToolName, namespacedTool),
		attribute.String(telemetry.AttrMCPMethod, "tools/call"),
	)

	if err := validate.CheckArgumentJSON(argsJSON, a.argLimits); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "argument limits")
		if errors.Is(err, validate.ErrArgumentsTooLarge) {
			telemetry.RecordPayloadBytesRejected(ctx, defaults.MetricBytesRejectReasonToolArgs)
		}
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageLimits, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil)
	}
	telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageLimits, defaults.MetricArgsResultPass)

	a.schemaMu.RLock()
	sch := a.toolValidators[namespacedTool]
	a.schemaMu.RUnlock()

	var pol *policy.Engine
	if a.policyHolder != nil {
		pol = a.policyHolder.Load()
	}
	if pol != nil && pol.RequiresStrictSchema(namespacedTool) && sch == nil {
		err := fmt.Errorf("tool %q requires input schema (elevated policy)", namespacedTool)
		span.RecordError(err)
		span.SetStatus(codes.Error, "elevated schema required")
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil)
	}
	if sch == nil {
		span.SetStatus(codes.Ok, "")
		return nil
	}
	var inst any
	if err := json.Unmarshal(argsJSON, &inst); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "arguments json")
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, "invalid JSON arguments", nil)
	}
	if err := sch.Validate(inst); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "json schema")
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, hostVisibleJSONSchemaError(err), nil)
	}
	telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultPass)
	span.SetStatus(codes.Ok, "")
	return nil
}
