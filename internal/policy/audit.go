package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/KarmaXP/mcp-gateway/internal/telemetry"
)

// Audit keys — structured security audit (SEC5: no tokens, payloads, or argument bodies).
const (
	AuditMessageKey = "mcp_security_audit"
)

// LogAudit emits a structured audit record for allow/deny decisions.
// subjectID is hashed; toolName and reason must not contain secrets or argument data.
func LogAudit(ctx context.Context, outcome, reason, toolName, subjectID, policyVersion string) {
	if ctx == nil {
		ctx = context.Background()
	}
	subHash := HashSubject(subjectID)
	attrs := []any{
		AuditMessageKey, true,
		"outcome", outcome,
		"reason", reason,
		"policy_version", policyVersion,
		"subject_sha256_8", subHash,
	}
	if toolName != "" {
		attrs = append(attrs, "tool_name", toolName)
	}
	slog.InfoContext(ctx, "mcp policy decision", attrs...)
	telemetry.RecordPolicyDecision(ctx, outcome, reason)
}

// HashSubject returns an 8-hex-char prefix of SHA-256(sub) for logs (O5: no raw user IDs in high-cardinality metrics; logs use truncated hash).
func HashSubject(subjectID string) string {
	if subjectID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(subjectID))
	return hex.EncodeToString(sum[:])[:8]
}
