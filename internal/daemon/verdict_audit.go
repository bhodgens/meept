package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/memory"
)

// verdictAuditor adapts *auditlog.Store to memory.VerdictAuditor (leaf
// T03-L1). The memory package must not import internal/auditlog, so the
// adapter lives here and is injected at daemon startup via
// Manager.SetVerdictAuditor — mirroring the internal/employee injection
// pattern (internal/employee/wiring.go SetChainEmitter).
//
// Every verdict appends one canonical-JSON chain record:
//
//	{action: "memory.claim.verdict", claim_id, verdict, reason_hash}
//
// reason_hash is memory.ReasonHash(reason) (first 16 hex of sha256); raw
// reason text never enters the chain. RecordVerdict is called by the Manager
// BEFORE the status transition returns, so the audit entry is durable-then-
// acknowledged. A record type is set (required by auditlog.Store.Append) and
// the payload passes through SanitizePayload like every other emitter.
type verdictAuditor struct {
	store  *auditlog.Store
	logger *slog.Logger
}

// newVerdictAuditor returns a memory.VerdictAuditor over store, or nil when
// store is nil (auditing disabled — best-effort by contract).
func newVerdictAuditor(store *auditlog.Store, logger *slog.Logger) memory.VerdictAuditor {
	if store == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &verdictAuditor{store: store, logger: logger.With("component", "memory-verdict-audit")}
}

// RecordVerdict implements memory.VerdictAuditor.
func (a *verdictAuditor) RecordVerdict(claimID, verdict, reasonHash string) error {
	if a == nil || a.store == nil {
		return nil
	}
	rec := auditlog.Record{
		Type: memory.VerdictAction,
		Payload: auditlog.SanitizePayload(map[string]any{
			"action":      memory.VerdictAction,
			"claim_id":    claimID,
			"verdict":     verdict,
			"reason_hash": reasonHash,
		}),
	}
	if _, err := a.store.Append(context.Background(), rec); err != nil {
		return fmt.Errorf("append verdict audit record: %w", err)
	}
	return nil
}

// wireVerdictAuditor injects the auditlog-backed verdict auditor into the
// memory manager at daemon startup. The chain store is opened next to the
// memory data dir (audit_chain.db) unless one is already wired on the shared
// employee DB — auditing failure is non-fatal: a nil auditor simply disables
// audit entries (best-effort, never blocks a verdict).
func wireVerdictAuditor(memMgr *memory.Manager, dataDir string, logger *slog.Logger) {
	if memMgr == nil {
		return
	}
	store, err := auditlog.OpenStore(dataDir, logger)
	if err != nil {
		if logger != nil {
			logger.Warn("memory verdict auditing disabled: audit chain store init failed",
				"component", "memory-verdict-audit", "error", err)
		}
		return
	}
	if auditor := newVerdictAuditor(store, logger); auditor != nil {
		memMgr.SetVerdictAuditor(auditor)
		if logger != nil {
			logger.Info("memory verdict auditing wired", "component", "memory-verdict-audit")
		}
	}
}
