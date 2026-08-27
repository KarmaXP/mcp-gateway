package multiplex

import (
	"context"
	"errors"
)

const (
	PartialFailureTransport = "transport_error"
	PartialFailureJSONRPC = "jsonrpc_error"
	PartialFailureTimeout = "timeout"
	PartialFailureOmitted = "omitted"
)

type PartialFailure struct {
	UpstreamID string `json:"backend_id"`
	Reason     string `json:"reason"`
}

func classifyCallFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return PartialFailureTimeout
	}
	return PartialFailureTransport
}

func partialFailuresToMaps(failures []PartialFailure) []map[string]any {
	out := make([]map[string]any, len(failures))
	for i, f := range failures {
		out[i] = map[string]any{
			"backend_id": f.UpstreamID,
			"reason":     f.Reason,
		}
	}
	return out
}

func attachInitPartialFailures(merged map[string]any, failures []PartialFailure) {
	if len(failures) == 0 {
		return
	}
	serverInfo, _ := merged["serverInfo"].(map[string]any)
	if serverInfo == nil {
		return
	}
	extras, ok := serverInfo["extras"].(map[string]any)
	if !ok {
		extras = map[string]any{}
		serverInfo["extras"] = extras
	}
	extras["partial_failures"] = partialFailuresToMaps(failures)
}

func attachListExtrasPartialFailures(payload map[string]any, failures []PartialFailure) {
	if len(failures) == 0 {
		return
	}
	extras, ok := payload["extras"].(map[string]any)
	if !ok {
		extras = map[string]any{}
		payload["extras"] = extras
	}
	extras["partial_failures"] = partialFailuresToMaps(failures)
}
