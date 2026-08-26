// Package hostctx stores per-request values on context (intent, subject, allow list).
// Allow list: nil -> unrestricted; empty slice -> deny-all; non-empty -> restricted names.
package hostctx

import (
	"context"
	"encoding/json"
	"strings"
)

// Optional natural-language hint for semantic routing (HTTP header name).
const HeaderMCPIntent = "X-MCP-Intent"

// Optional host-provided token usage metadata for this RPC ingress request.
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

// AllowListMode is how JWT/RAR allow-list state is stored in context.
type AllowListMode int

const (
	AllowListUnrestricted AllowListMode = iota
	AllowListDenyAll
	AllowListRestricted
)

type allowListState struct {
	mode  AllowListMode
	names []string
}

func WithAllowList(parent context.Context, toolNames []string) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if toolNames == nil {
		return parent
	}
	if len(toolNames) == 0 {
		return context.WithValue(parent, allowedToolNamesKey{}, allowListState{mode: AllowListDenyAll})
	}
	cp := normalizeAllowList(toolNames)
	if len(cp) == 0 {
		return context.WithValue(parent, allowedToolNamesKey{}, allowListState{mode: AllowListDenyAll})
	}
	return context.WithValue(parent, allowedToolNamesKey{}, allowListState{
		mode:  AllowListRestricted,
		names: cp,
	})
}

func AllowListModeFromContext(ctx context.Context) (AllowListMode, []string) {
	if ctx == nil {
		return AllowListUnrestricted, nil
	}
	st, ok := ctx.Value(allowedToolNamesKey{}).(allowListState)
	if !ok {
		return AllowListUnrestricted, nil
	}
	switch st.mode {
	case AllowListUnrestricted:
		return AllowListUnrestricted, nil
	case AllowListDenyAll:
		return AllowListDenyAll, []string{}
	case AllowListRestricted:
		return AllowListRestricted, append([]string(nil), st.names...)
	}
	// A mode this function does not know denies, so adding one cannot widen access.
	return AllowListDenyAll, []string{}
}

type subjectIDKey struct{}

// WithSubjectID attaches JWT sub for audit (hash before logging).
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

// MergeRequestValues copies host-scoped values from req onto parent.
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

func normalizeAllowList(in []string) []string {
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

const jsonNullLiteral = "null"

type hostInitializeParamsKey struct{}

func WithHostInitializeParams(ctx context.Context, params json.RawMessage) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(params) == 0 || string(params) == jsonNullLiteral {
		return ctx
	}
	return context.WithValue(ctx, hostInitializeParamsKey{}, append(json.RawMessage(nil), params...))
}

func HostInitializeParams(ctx context.Context) json.RawMessage {
	if ctx == nil {
		return nil
	}
	raw, _ := ctx.Value(hostInitializeParamsKey{}).(json.RawMessage)
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
