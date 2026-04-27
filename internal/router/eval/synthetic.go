package eval

import (
	"github.com/KarmaXP/mcp-gateway/internal/router"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
)

// SyntheticCatalog returns ≥20 tools with distinct descriptions for Phase-2 style benchmarks.
func SyntheticCatalog() []router.CatalogEntry {
	rows := []struct {
		prefix, native, desc string
		keys                 []string
	}{
		{"k8s", "get_logs", "Stream container logs from a Kubernetes pod for debugging incidents", []string{"pod", "namespace", "tail"}},
		{"k8s", "list_pods", "List pods in a namespace with status and resource usage summary", []string{"namespace", "label_selector"}},
		{"k8s", "apply_manifest", "Apply a declarative YAML manifest to a Kubernetes cluster safely", []string{"manifest_path", "dry_run"}},
		{"k8s", "rollout_status", "Check rollout progress for a Deployment or StatefulSet resource", []string{"name", "namespace"}},
		{"aws", "list_buckets", "Enumerate S3 buckets visible to the current IAM principal", []string{"region"}},
		{"aws", "put_object", "Upload an object to an S3 bucket with optional server-side encryption", []string{"bucket", "key", "body"}},
		{"aws", "describe_instances", "Describe EC2 instances filtered by tags or VPC identifiers", []string{"filters"}},
		{"aws", "rotate_keys", "Rotate IAM access keys for a service account following policy", []string{"user"}},
		{"gh", "list_prs", "List open pull requests for a GitHub repository with labels", []string{"repo", "state"}},
		{"gh", "merge_pr", "Merge an approved pull request using squash or merge commit", []string{"repo", "number"}},
		{"gh", "request_review", "Request reviewers on a pull request with a comment template", []string{"repo", "number", "reviewers"}},
		{"slack", "post_message", "Post a formatted message to a Slack channel with thread support", []string{"channel", "text"}},
		{"slack", "list_channels", "List Slack channels the bot can access for routing notifications", []string{"cursor"}},
		{"jira", "create_issue", "Create a Jira issue with project key, summary, and issue type", []string{"project", "summary"}},
		{"jira", "transition_issue", "Move a Jira issue through workflow states with a comment", []string{"key", "transition"}},
		{"pg", "run_query", "Execute a read-only SQL query against a Postgres analytics replica", []string{"sql", "timeout"}},
		{"pg", "list_tables", "List tables in a Postgres schema for discovery workflows", []string{"schema"}},
		{"prom", "query_range", "Query Prometheus metrics over a time range with step resolution", []string{"query", "start", "end"}},
		{"prom", "targets", "List Prometheus scrape targets and health for observability checks", []string{"state"}},
		{"vault", "read_secret", "Read a secret path from Vault with version pinning support", []string{"path", "version"}},
		{"vault", "renew_lease", "Renew a Vault lease before expiry for long-running automation", []string{"lease_id"}},
		{"dns", "lookup_record", "Resolve DNS records for incident triage and connectivity checks", []string{"name", "type"}},
		{"tls", "check_cert", "Inspect TLS certificate expiry for a hostname and port combination", []string{"host", "port"}},
		{"run", "shell_command", "Run an audited shell command on a bastion with allow-listed binaries", []string{"command", "timeout"}},
	}
	out := make([]router.CatalogEntry, 0, len(rows))
	for _, r := range rows {
		full := r.prefix + "__" + r.native
		out = append(out, router.CatalogEntry{
			ToolRow: index.ToolRow{
				Name:        full,
				Description: r.desc,
				ParamKeys:   r.keys,
			},
			BackendID: "b_" + r.prefix,
		})
	}
	return out
}

// GoldenCases returns intent phrases; WantTool is the namespaced tool id the router should select.
func GoldenCases() []struct {
	Intent   string
	WantTool string
	Allowed  []string
} {
	cat := SyntheticCatalog()
	cases := make([]struct {
		Intent   string
		WantTool string
		Allowed  []string
	}, 0, len(cat))
	for _, e := range cat {
		// Use description wording so lexical overlap with indexed document is high.
		intent := e.ToolRow.Description
		cases = append(cases, struct {
			Intent   string
			WantTool string
			Allowed  []string
		}{Intent: intent, WantTool: e.ToolRow.Name})
	}
	return cases
}
