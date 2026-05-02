package multiplex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.opentelemetry.io/otel/attribute"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/policy"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
	"github.com/KarmaXP/mcp-gateway/internal/validate"
)

func filterToolsForPolicy(merged []map[string]any, allowed []string) ([]map[string]any, error) {
	if len(allowed) == 0 {
		return merged, nil
	}
	out := make([]map[string]any, 0, len(merged))
	for _, t := range merged {
		name, _ := t["name"].(string)
		ok, err := policy.AllowedListContains(name, allowed)
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
	for _, t := range merged {
		name, _ := t["name"].(string)
		if name == "" {
			continue
		}
		sch, ok := t["inputSchema"]
		if !ok || sch == nil {
			continue
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
	span.SetAttributes(attribute.String("mcp.tool.name", namespacedTool))

	if err := validate.CheckArgumentJSON(argsJSON, a.argLimits); err != nil {
		span.RecordError(err)
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageLimits, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil)
	}
	telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageLimits, defaults.MetricArgsResultPass)

	a.schemaMu.RLock()
	sch := a.toolValidators[namespacedTool]
	a.schemaMu.RUnlock()

	if a.policyEngine != nil && a.policyEngine.RequiresStrictSchema(namespacedTool) && sch == nil {
		err := fmt.Errorf("tool %q requires input schema (elevated policy)", namespacedTool)
		span.RecordError(err)
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, err.Error(), nil)
	}
	if sch == nil {
		return nil
	}
	var inst any
	if err := json.Unmarshal(argsJSON, &inst); err != nil {
		span.RecordError(err)
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, "invalid JSON arguments", nil)
	}
	if err := sch.Validate(inst); err != nil {
		span.RecordError(err)
		telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultFail)
		return rpc.NewError(hostID, errcodes.InvalidParams, hostVisibleJSONSchemaError(err), nil)
	}
	telemetry.RecordToolArgsValidation(ctx, defaults.MetricArgsStageSchema, defaults.MetricArgsResultPass)
	return nil
}
