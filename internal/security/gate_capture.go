package security

import "context"

// GateAskCapture is the chokepoint-2 capture seam (jev-command-gate leaf 02,
// Contract C4): the shell permission-table ask -> confirmation-resolution
// capture sink. Declared in internal/security as a narrow interface and
// satisfied by *gatecapture.Store (whose concrete methods carry context) -
// the engine never imports internal/gatecapture, keeping the same seam
// direction as every other capture sink.
//
// LOCK CONTRACT: PermissionTableAsk is called from checkShellPermissionTable
// while the Engine holds mu as RLock. The implementation MUST be memory-only
// (never sqlite I/O, never a call back into Engine methods that acquire mu).
// *gatecapture.Store satisfies this: its arm path is a mutex-guarded in-map
// insert that never touches the DB.
type GateAskCapture interface {
	// PermissionTableAsk arms the pending capture for a permission-table ask.
	PermissionTableAsk(command string)
	// PermissionTableResolved lands the record at the confirmation
	// resolution. Called OUTSIDE any engine lock, from the wiring layer.
	PermissionTableResolved(ctx context.Context, command, decision, approver string)
}
