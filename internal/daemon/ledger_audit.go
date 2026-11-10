package daemon

// ledger_audit.go — branch 04-ops impl leaf 04-02: audit-chain rows for
// state-changing ledger appends, folded into 03-01's after-append hook
// composition (02-audit-entries.md tasks 1-2).
//
// Frozen contract (02-audit-entries.md):
//   - st != nil (status-carrying append) -> exactly one chain row
//     {type: "ledger.append", employee_id, payload {ledger_id, kind,
//     subject, source, source_ref, status}} written via the shared
//     chain handle (ledgerAuditChain resolves it over the same DB pool
//     the voice dial auditWriterAdapter.WriteAudit uses).
//   - st == nil (log-only row: observation/committal volume) -> never
//     touches the chain; the chain stays a security trail (master C
//     decision, task 2).
//   - nil chain handle -> no-op hook; appends succeed unchanged.
//   - Hook failures are WARN-only and can never surface to Append: the
//     row is already committed and the publish hook already ran when
//     this fires.

import (
	"context"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/ledger"
)

// ledgerAuditChainType is the frozen chain-row discriminator (04-02:
// type:'ledger.append').
const ledgerAuditChainType = "ledger.append"

// ledgerAuditDrain bounds the post-commit chain write. The hook runs
// synchronously on the Append goroutine against the single-connection
// chain pool (busy_timeout 5000 on the DSN); the cap keeps a wedged
// chain from stalling appends indefinitely.
const ledgerAuditDrain = 5 * time.Second

// buildLedgerAuditHook returns the after-append hook that writes one
// audit-chain row per state-changing append. Installed by 03-01's
// buildLedger composition AFTER the S4 publish hook; a nil chain store
// yields a no-op hook (never panics).
func buildLedgerAuditHook(chain *auditlog.Store, logger *slog.Logger) func(rec ledger.Record, st *ledger.State) {
	if logger == nil {
		logger = slog.Default()
	}
	if chain == nil {
		return func(ledger.Record, *ledger.State) {}
	}
	return func(rec ledger.Record, st *ledger.State) {
		if st == nil {
			// Task 2: log-only rows never hit the chain.
			return
		}
		payload := map[string]any{
			"ledger_id":  rec.ID,
			"kind":       string(rec.Kind),
			"subject":    rec.Subject,
			"source":     string(rec.Source),
			"source_ref": rec.SourceRef,
			"status":     string(st.Status),
		}
		ctx, cancel := context.WithTimeout(context.Background(), ledgerAuditDrain)
		defer cancel()
		if _, err := chain.Append(ctx, auditlog.Record{
			Type:       ledgerAuditChainType,
			EmployeeID: rec.EmployeeID,
			Payload:    payload,
		}); err != nil {
			// Post-commit: WARN-only, never surfaced to Append callers.
			logger.Warn("ledger audit: chain append failed",
				"employee_id", rec.EmployeeID, "ledger_id", rec.ID, "error", err)
		}
	}
}

// composeLedgerAfterAppendHooks fuses the hooks into the single func
// ledger.Store.WithAfterAppend accepts, firing them in the given order
// (publish first, audit chained second per 02-audit-entries task 1).
// Nil entries are skipped; a hook never aborts its siblings — each does
// its own WARN-only error handling, so publish ordering survives a
// chain-write failure.
func composeLedgerAfterAppendHooks(hooks ...func(rec ledger.Record, st *ledger.State)) func(ledger.Record, *ledger.State) {
	joined := make([]func(ledger.Record, *ledger.State), 0, len(hooks))
	for _, h := range hooks {
		if h != nil {
			joined = append(joined, h)
		}
	}
	return func(rec ledger.Record, st *ledger.State) {
		for _, h := range joined {
			h(rec, st)
		}
	}
}
