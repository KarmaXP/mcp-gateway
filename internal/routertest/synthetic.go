package routertest

import (
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
)

func SyntheticCatalog() []router.IndexedTool {
	rows := []struct {
		prefix, native, desc string
		keys                 []string
	}{
		{"k8s", "get_pod_logs", "Get Kubernetes pod logs for an incident window and optional previous container stream", []string{"namespace", "pod", "container", "since", "previous", "tail"}},
		{"k8s", "list_pods", "List Kubernetes pods in a namespace with readiness, restart count, and node placement", []string{"namespace", "label_selector", "field_selector"}},
		{"k8s", "describe_pod", "Inspect one Kubernetes pod status with container conditions readiness probes and restart history", []string{"namespace", "pod"}},
		{"k8s", "list_nodes", "List Kubernetes nodes with schedulable state, pressure conditions, and kubelet version", []string{"label_selector"}},
		{"k8s", "describe_node", "Describe a Kubernetes node allocatable resources and recent node condition transitions", []string{"node"}},
		{"k8s", "rollout_status", "Check Kubernetes deployment rollout status and unavailable replica progress", []string{"namespace", "deployment", "timeout"}},
		{"k8s", "restart_deployment", "Restart a Kubernetes deployment by patching pod template annotations safely", []string{"namespace", "deployment", "reason"}},
		{"k8s", "list_events", "List namespace-scoped Kubernetes warning and error events sorted by last seen time", []string{"namespace", "type", "limit"}},
		{"prom", "query_instant", "Run an instant Prometheus query for a single evaluation timestamp", []string{"query", "time"}},
		{"prom", "query_range", "Run a Prometheus range query over start end and step for SLO burn analysis", []string{"query", "start", "end", "step"}},
		{"prom", "label_values", "Fetch Prometheus label values to scope dashboard filters during triage", []string{"label", "match"}},
		{"prom", "series", "List Prometheus series matching metric selectors across a bounded time range", []string{"match", "start", "end"}},
		{"prom", "targets", "List Prometheus scrape targets and health errors grouped by job", []string{"state"}},
		{"prom", "alerts", "List active Prometheus alerts with severity, state, and fingerprint metadata", []string{"state", "receiver"}},
		{"prom", "rules", "List Prometheus alerting and recording rules with evaluation interval details", []string{"group"}},
		{"logs", "query", "Query centralized logs by service, namespace, and free text over a time window", []string{"service", "namespace", "query", "start", "end", "limit"}},
		{"logs", "tail", "Tail centralized logs for a service with stream follow and sampling controls", []string{"service", "namespace", "query", "follow"}},
		{"logs", "context", "Fetch surrounding centralized log lines around a specific log record identifier", []string{"record_id", "before", "after"}},
		{"logs", "facets", "List centralized log facets and top values for fast incident slicing", []string{"field", "start", "end"}},
		{"logs", "saved_searches", "List saved centralized log searches used in on call runbooks", []string{"team"}},
		{"logs", "pipelines", "List centralized log processing pipelines and drop filter configuration", []string{"environment"}},
		{"logs", "ingestion_health", "Show centralized log ingestion lag and dropped record counters by source", []string{"source", "start", "end"}},
		{"gh", "list_prs", "List GitHub pull requests for a repository to correlate deploys with incidents", []string{"owner", "repo", "state", "limit"}},
		{"gh", "get_pr", "Get GitHub pull request details including checks and merge state", []string{"owner", "repo", "number"}},
		{"gh", "list_issues", "List GitHub issues filtered by labels and state for incident triage", []string{"owner", "repo", "labels", "state"}},
		{"gh", "search_code", "Search GitHub code in a repository for error strings and stack trace tokens", []string{"owner", "repo", "query"}},
	}
	out := make([]router.IndexedTool, 0, len(rows))
	for _, r := range rows {
		full := r.prefix + "__" + r.native
		out = append(out, router.IndexedTool{
			ToolRow: index.ToolRow{
				Name:        full,
				Description: r.desc,
				ParamKeys:   r.keys,
			},
			UpstreamID: "b_" + r.prefix,
		})
	}
	return out
}

type GoldenCase struct {
	Intent    string
	WantTool  string
	Allowed   []string
	Relevance map[string]float64
}

func GoldenCases() []GoldenCase {
	cat := SyntheticCatalog()
	toolsByPrefix := make(map[string][]string, len(cat))
	for _, e := range cat {
		pfx := e.ToolRow.Name
		for i := 0; i < len(pfx)-1; i++ {
			if pfx[i] == '_' && pfx[i+1] == '_' {
				pfx = pfx[:i]
				break
			}
		}
		toolsByPrefix[pfx] = append(toolsByPrefix[pfx], e.ToolRow.Name)
	}
	cases := make([]GoldenCase, 0, len(cat))
	for _, e := range cat {
		intent := e.ToolRow.Description
		relevance := map[string]float64{e.ToolRow.Name: 3}
		pfx := e.ToolRow.Name
		for i := 0; i < len(pfx)-1; i++ {
			if pfx[i] == '_' && pfx[i+1] == '_' {
				pfx = pfx[:i]
				break
			}
		}
		for _, other := range toolsByPrefix[pfx] {
			if other == e.ToolRow.Name {
				continue
			}
			relevance[other] = 1
		}
		cases = append(cases, GoldenCase{
			Intent:    intent,
			WantTool:  e.ToolRow.Name,
			Relevance: relevance,
		})
	}
	return cases
}
