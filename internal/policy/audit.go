package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Log attribute key for policy audit records (no secrets in attrs).
const AuditMessageKey = "mcp_security_audit"

const subjectPseudonymHexLen = 16

type Decision struct {
	Outcome       string
	Reason        string
	ToolName      string
	SubjectID     string
	PolicyVersion string
}

type Auditor struct {
	sink   AuditSink
	pepper []byte
}

func NewAuditor(sink AuditSink, subjectPepper []byte) *Auditor {
	if sink == nil {
		return nil
	}
	return &Auditor{sink: sink, pepper: subjectPepper}
}

// A nil Auditor records nothing, so a caller that was never given one stays silent.
func (a *Auditor) Record(ctx context.Context, d Decision) {
	if a == nil || a.sink == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = a.sink.Emit(ctx, AuditRecord{
		Outcome:       d.Outcome,
		Reason:        d.Reason,
		ToolName:      d.ToolName,
		SubjectSHA256: a.pseudonym(d.SubjectID),
		PolicyVersion: d.PolicyVersion,
		At:            time.Now(),
	})
}

// A pseudonym is keyed so low-entropy subjects cannot be brute forced from the logs,
// and 64 bits wide so it stays unique across a deployment's population.
func (a *Auditor) pseudonym(subjectID string) string {
	if subjectID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, a.pepper)
	_, _ = mac.Write([]byte(subjectID))
	return hex.EncodeToString(mac.Sum(nil))[:subjectPseudonymHexLen]
}
