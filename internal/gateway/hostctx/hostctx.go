package hostctx

import (
	"context"
	"strings"
)

// Optional natural-language hint for semantic routing (HTTP header name).
const HeaderMCPIntent = "X-MCP-Intent"

// Optional host-provided LLM token usage metadata for this RPC ingress request.
const HeaderAgentTokensUsed = "X-Agent-Tokens-Used"

type clientIntentKey struct{}

func WithClientIntent(ctx context.Context, intent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, clientIntentKey{}, strings.TrimSpace(intent))
}

func ClientIntentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(clientIntentKey{}).(string)
	return s
}

type allowedToolNamesKey struct{}

// AllowListMode describes JWT/RAR merged allow-list propagation (ADR 0003).
type AllowListMode int

const (
	// AllowListUnrestricted: no allow-list in context (full catalog for principal).
	AllowListUnrestricted AllowListMode = iota
	// AllowListDenyAll — explicit empty intersection or empty policy list (deny every tool).
	AllowListDenyAll
	// AllowListRestricted — non-empty allow-list.
	AllowListRestricted
)

type allowListState struct {
	mode  AllowListMode
	names []string
}

// AttachPolicyAllowList records the effective JWT/RAR allow list for this authenticated HTTP request.
// toolNames nil ⇒ explicit unrestricted (overwrites a restricted parent on merge). Non-nil empty ⇒ deny-all.
func AttachPolicyAllowList(parent context.Context, toolNames []string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if toolNames == nil {
		return context.WithValue(parent, allowedToolNamesKey{}, allowListState{mode: AllowListUnrestricted})
	}
	return WithAllowedToolNames(parent, toolNames)
}

// WithAllowedToolNames attaches the merged JWT/RAR allow list.
// toolNames nil ⇒ unrestricted (no value stored). Non-nil empty slice ⇒ deny-all.
func WithAllowedToolNames(parent context.Context, toolNames []string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if toolNames == nil {
		return parent
	}
	if len(toolNames) == 0 {
		return context.WithValue(parent, allowedToolNamesKey{}, allowListState{mode: AllowListDenyAll})
	}
	cp := normalizeAllowedToolNames(toolNames)
	if len(cp) == 0 {
		return context.WithValue(parent, allowedToolNamesKey{}, allowListState{mode: AllowListDenyAll})
	}
	return context.WithValue(parent, allowedToolNamesKey{}, allowListState{
		mode:  AllowListRestricted,
		names: cp,
	})
}

// AllowListModeFromContext returns how tools/list and tools/call should apply AuthZ.
func AllowListModeFromContext(ctx context.Context) (AllowListMode, []string) {
	if ctx == nil {
		return AllowListUnrestricted, nil
	}
	st, ok := ctx.Value(allowedToolNamesKey{}).(allowListState)
	if !ok {
		return AllowListUnrestricted, nil
	}
	switch st.mode {
	case AllowListDenyAll:
		return AllowListDenyAll, []string{}
	case AllowListRestricted:
		return AllowListRestricted, append([]string(nil), st.names...)
	default:
		return AllowListUnrestricted, nil
	}
}

// AllowedToolNamesFromContext returns the allow-list slice for policy checks and router signals.
// nil ⇒ unrestricted; non-nil empty ⇒ deny-all; otherwise a copy of restricted names.
func AllowedToolNamesFromContext(ctx context.Context) []string {
	_, names := AllowListModeFromContext(ctx)
	return names
}

type subjectIDKey struct{}

// JWT sub for audit paths: hash before logging (no secrets in audit attrs).
func WithSubjectID(parent context.Context, subject string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return parent
	}
	return context.WithValue(parent, subjectIDKey{}, subject)
}

func SubjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(subjectIDKey{}).(string)
	return s
}

type policyVersionKey struct{}

func WithPolicyVersion(parent context.Context, version string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return parent
	}
	return context.WithValue(parent, policyVersionKey{}, version)
}

func PolicyVersionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(policyVersionKey{}).(string)
	return s
}

// MergeRequestValues copies host-scoped values from req onto parent. Parent cancellation and deadlines are unchanged.
func MergeRequestValues(parent, req context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if req == nil || req == parent {
		return parent
	}
	out := parent
	if _, ok := req.Value(clientIntentKey{}).(string); ok {
		out = WithClientIntent(out, ClientIntentFromContext(req))
	}
	if st, ok := req.Value(allowedToolNamesKey{}).(allowListState); ok {
		out = context.WithValue(out, allowedToolNamesKey{}, st)
	}
	if sub := SubjectIDFromContext(req); sub != "" {
		out = WithSubjectID(out, sub)
	}
	if ver := PolicyVersionFromContext(req); ver != "" {
		out = WithPolicyVersion(out, ver)
	}
	return out
}

func normalizeAllowedToolNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
