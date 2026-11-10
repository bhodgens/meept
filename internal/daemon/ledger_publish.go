package daemon

// ledger_publish.go — branch 02-surfaces impl leaf 02-surfaces/03:
// the S4 publish hook installed on the ledger Store via
// Store.WithAfterAppend (contract §3 / 03-publish.md tasks 1-3).
//
// Body contract (frozen):
//   - nil bus -> no-op hook (never panics; appends keep working).
//   - State non-nil (status-carrying row): publish RecordChanged{
//     Store: "ledger", Key: emp+"/"+subject, Op: ChangeOpPut,
//     Value: <State JSON>}.
//   - State nil (log-only row): Key = emp+"/" (trailing slash per
//     03-publish task 2) and Value = the Record JSON — S4 says every
//     durable write announces and the event "carries the new snapshot";
//     for a log-only row the new immutable Record IS the snapshot.
//
// Delivery uses bus.PublishT on the FROZEN storage.StorageRecordChanged
// Topic ("storage.record.changed") — never a new topic, never
// PublishBlockingT: drop-on-full is acceptable for announcements because
// durability lives in SQLite, and the hook runs synchronously on the
// Append goroutine right after COMMIT (blocking delivery could stall a
// subscriber against the single-connection WAL pool).

import (
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/storage"
)

// ledgerStoreName is the RecordChanged.Store discriminator for the
// employee ledger (03-publish task 2 / S4 precedent set by the store
// packages).
const ledgerStoreName = "ledger"

// buildLedgerAfterAppendHook returns the func to pass to
// ledger.Store.WithAfterAppend at Store construction (consumed by
// 03-voice/01's wiring). One event per successful Append, zero per
// rejected append (the Store only fires the hook after COMMIT).
func buildLedgerAfterAppendHook(msgBus *bus.MessageBus, logger *slog.Logger) func(rec ledger.Record, st *ledger.State) {
	if logger == nil {
		logger = slog.Default()
	}
	if msgBus == nil {
		// Task 1: nil bus -> no-op hook, never panics.
		return func(ledger.Record, *ledger.State) {}
	}
	return func(rec ledger.Record, st *ledger.State) {
		event := storage.RecordChanged{
			Store: ledgerStoreName,
			Op:    storage.ChangeOpPut,
		}
		if st != nil {
			// Status-carrying row: announce the new State snapshot
			// under the subject key emp+"/"+subject.
			value, err := json.Marshal(st)
			if err != nil {
				logger.Warn("ledger publish: marshal state failed",
					"employee_id", rec.EmployeeID, "subject", st.Subject, "error", err)
				return
			}
			event.Key = rec.EmployeeID + "/" + st.Subject
			event.Value = value
		} else {
			// Log-only row: S4 "carries the new snapshot" — the Record
			// JSON is the snapshot; key is emp+"/" per 03-publish task 2.
			value, err := json.Marshal(rec)
			if err != nil {
				logger.Warn("ledger publish: marshal record failed",
					"employee_id", rec.EmployeeID, "record_id", rec.ID, "error", err)
				return
			}
			event.Key = rec.EmployeeID + "/"
			event.Value = value
		}
		// PublishT (NOT PublishBlockingT — 03-publish task 3): announcements
		// are drop-on-full acceptable; SQLite is the durability source.
		bus.PublishT(msgBus, storage.StorageRecordChangedTopic, ledgerStoreName, event)
	}
}
