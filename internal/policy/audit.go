package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Log attribute key for policy audit records (no secrets in attrs).
const AuditMessageKey = "mcp_security_audit"

func LogAudit(ctx context.Context, outcome, reason, toolName, subjectID, policyVersion string) {
	if ctx == nil {
		ctx = context.Background()
	}
	rec := AuditRecord{
		Outcome:       outcome,
		Reason:        reason,
		ToolName:      toolName,
		SubjectSHA256: HashSubject(subjectID),
		PolicyVersion: policyVersion,
		At:            time.Now(),
	}
	_ = currentAuditSink().Emit(ctx, rec)
}

func HashSubject(subjectID string) string {
	if subjectID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(subjectID))
	return hex.EncodeToString(sum[:])[:8]
}
