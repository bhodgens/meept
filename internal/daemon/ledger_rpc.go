package daemon

// ledger_rpc.go — the daemon-side adapter registering the
// ledger.query / ledger.state RPC surface (plan 20261109-employee-ledger,
// leaf 02-surfaces/02; bughunt D6).
//
// The handler lives in internal/rpc (which must not import
// internal/ledger — the voice_local.go precedent); the DAEMON adapts the
// concrete *ledger.Store onto the narrow rpc.LedgerReader seam and maps
// ledger row types onto the handler's local wire types here. The
// registration site is daemon.go's RPC block, next to
// registerVoiceLocalRPC, exactly where internal/rpc/ledger.go's wiring
// note points. Always registers (a nil store is the handler's
// ErrLedgerDisabled envelope, never "method not found").
//
// The store's Head seam is the store-wide max id (QueryLog's LIMIT caps
// at 1000, so a full scan for a head is unnecessary — SELECT MAX(id)).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/rpc"
)

// registerLedgerRPC registers ledger.query / ledger.state on the RPC
// server over the daemon's employee ledger store. Nil-safe end to end:
// a nil server or components is a no-op; a nil STORE still registers —
// the handler then refuses every call with rpc.ErrLedgerDisabled (the
// disabled-store posture, matching registerVoiceLocalRPC's "always
// registers" contract).
func registerLedgerRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	var store *ledger.Store
	if c != nil {
		store = c.EmployeeLedger
	}
	handler := rpc.NewLedgerHandler(ledgerReaderAdapter{store: store})
	handler.RegisterLedgerMethods(server)
	if store != nil {
		logger.Info("ledger RPC handlers registered")
	} else {
		logger.Info("ledger RPC handlers registered (disabled store: queries refuse)")
	}
}

// ledgerReaderAdapter adapts *ledger.Store onto rpc.LedgerReader. A nil
// store answers every method with an error (the handler maps nothing —
// the nil-reader guard inside the handler is what produces the named
// refusal, so this adapter mirrors that posture by returning a
// disabled-shaped error the handler never sees: the handler's own nil
// check runs first because we always construct the adapter non-nil, so
// the store-nil check lives HERE).
type ledgerReaderAdapter struct {
	store *ledger.Store
}

// errLedgerDisabled mirrors the disabled posture at the adapter layer.
var errLedgerDisabled = fmt.Errorf("ledger: store is not wired")

// QueryLog implements rpc.LedgerReader.
func (a ledgerReaderAdapter) QueryLog(ctx context.Context, employeeID, subject string, sinceID int64, limit int) ([]rpc.LedgerLogEntry, error) {
	if a.store == nil {
		return nil, errLedgerDisabled
	}
	rows, err := a.store.QueryLog(ctx, ledger.LogQuery{
		EmployeeID: employeeID,
		Subject:    subject,
		SinceID:    sinceID,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]rpc.LedgerLogEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, rpc.LedgerLogEntry{
			ID:         r.ID,
			EmployeeID: r.EmployeeID,
			At:         r.At.Format(time.RFC3339),
			Kind:       string(r.Kind),
			Subject:    r.Subject,
			Payload:    json.RawMessage(r.Payload),
			Source:     string(r.Source),
			SourceRef:  r.SourceRef,
			Resolves:   r.Resolves,
		})
	}
	return out, nil
}

// QueryOpen implements rpc.LedgerReader.
func (a ledgerReaderAdapter) QueryOpen(ctx context.Context, employeeID string) ([]rpc.LedgerOpenState, error) {
	if a.store == nil {
		return nil, errLedgerDisabled
	}
	rows, err := a.store.QueryOpen(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]rpc.LedgerOpenState, 0, len(rows))
	for _, st := range rows {
		out = append(out, rpc.LedgerOpenState{
			EmployeeID: st.EmployeeID,
			Subject:    st.Subject,
			Status:     string(st.Status),
			Summary:    st.Summary,
			UpdatedAt:  st.UpdatedAt.Format(time.RFC3339),
			AsOf:       st.AsOf,
		})
	}
	return out, nil
}

// Head implements rpc.LedgerReader: the store-wide max ledger id.
func (a ledgerReaderAdapter) Head(ctx context.Context) (int64, error) {
	if a.store == nil {
		return 0, errLedgerDisabled
	}
	return a.store.QueryHead(ctx)
}
