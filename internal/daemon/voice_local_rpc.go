package daemon

// voice_local_rpc.go - the daemon-side adapter for the voice.local.*
// RPC surface (local-leg leaf 08): maps *voice.LocalLeg (+ registry +
// employee store + the per-call telemetry ring) onto rpc.VoiceLocalSeams.
// internal/rpc must not import internal/voice (the voice -> activity ->
// rpc chain), so the concrete types stop HERE.
//
// Every seam is nil-safe per the repo rule: no voice wiring, no local
// leg, or no employee manager degrades its METHOD to a clean named
// refusal (rpc.ErrVoiceLocalDisabled / rpc.ErrVoiceLocalUnknownEmployee
// via the handler's own gates), never a panic.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/voice"
)

// voiceLocalSeams adapts the wiring's voice pieces onto the RPC seam
// set. leg may be nil (voice.local.enabled false, wiring degraded): the
// returned seams are then all-nil = the handler's disabled posture, EXCEPT
// Events, which arms whenever the telemetry ring exists (a degraded local
// leg still carries phone-call rings worth reading).
func voiceLocalSeams(w *VoiceWiring, mgr *employee.Manager, logger *slog.Logger) rpc.VoiceLocalSeams {
	if w == nil {
		return rpc.VoiceLocalSeams{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if w.Local == nil {
		// Disabled leg: mutations refuse, but the ring (if built) still
		// answers voice.local.events for phone calls.
		return rpc.VoiceLocalSeams{Events: callEventsSeam(w)}
	}
	leg := w.Local
	registry := w.Registry

	open := func(ctx context.Context, employeeID, label string) (rpc.VoiceLocalOpen, error) {
		call, err := leg.Open(ctx, employeeID, label)
		if err != nil {
			return rpc.VoiceLocalOpen{}, mapVoiceLocalOpenErr(err)
		}
		return rpc.VoiceLocalOpen{
			CallID:    call.CallID,
			InPath:    call.InPath,
			OutPath:   call.OutPath,
			SessionID: call.SessionID,
		}, nil
	}

	end := func(callID, reason string) error {
		return leg.End(callID, reason)
	}

	status := func() []rpc.VoiceCallStatus {
		rows := []rpc.VoiceCallStatus{}
		seen := map[string]bool{}
		// Registry rows first: Open acquired BOTH legs there (CL6 shared
		// rules), so phone rows AND local rows (target "local:<label>",
		// employee resolved) come from this one snapshot - deduped by
		// call id so a local call is never listed twice (leg.Live()
		// below only fills gaps).
		//
		// DISCLOSURE (contact-display leaf 02 §4, O1/O4): phone-leg rows
		// carry the boundary-resolved display pair; the raw registry
		// Target is never copied to the wire. Local-leg "local:<label>"
		// targets have no number, so they pass through verbatim as the
		// display (O3 identifies them without digits-of-record concerns).
		if registry != nil {
			for _, inst := range registry.Live() {
				legName := inst.Leg
				if legName == "" {
					legName = voice.LegPhone
				}
				seen[inst.CallControlID] = true
				name, display := statusTargetDisplay(w, legName, inst.Target)
				rows = append(rows, rpc.VoiceCallStatus{
					CallID:        inst.CallControlID,
					EmployeeID:    inst.EmployeeID,
					TargetName:    name,
					TargetDisplay: display,
					SinceRFC3339:  "", // the registry carries no start time (C8); the CLI renders "-"
					Leg:           legName,
				})
			}
		}
		// Local leg rows NOT in the registry snapshot (defensive: Open
		// always acquires, but a mid-teardown race could observe the
		// pair here first). Target degrades to the "local:" prefix -
		// the label lives only in the registry row.
		for _, call := range leg.Live() {
			if seen[call.CallID] {
				continue
			}
			seen[call.CallID] = true
			rows = append(rows, rpc.VoiceCallStatus{
				CallID:        call.CallID,
				EmployeeID:    localEmployeeFor(registry, call.CallID),
				TargetName:    "local:",
				TargetDisplay: "local:",
				SinceRFC3339:  "",
				Leg:           voice.LegLocal,
			})
		}
		return rows
	}

	exists := func(ctx context.Context, employeeID string) (bool, error) {
		if mgr == nil {
			// No employee store wired: skip the existence gate (the
			// leg's own open gates still apply downstream).
			return true, nil
		}
		emp, err := mgr.GetEmployee(ctx, employeeID)
		if err != nil {
			if errors.Is(err, employee.ErrEmployeeNotFound) {
				return false, nil
			}
			return false, err
		}
		return emp != nil, nil
	}

	_ = logger
	return rpc.VoiceLocalSeams{
		Open:           open,
		End:            end,
		Status:         status,
		EmployeeExists: exists,
		Events:         callEventsSeam(w),
	}
}

// callEventsSeam adapts the wiring's per-call TelemetryRing onto the
// voice.local.events seam: a plain projection of the ring read onto the
// rpc row shape. A nil ring (wiring built without telemetry) returns all
// - false: the handler answers the empty envelope, never an error.
func callEventsSeam(w *VoiceWiring) func(callID string, cursor uint64, limit int) ([]rpc.VoiceCallEvent, bool, bool) {
	if w == nil || w.callEvents == nil {
		return nil
	}
	ring := w.callEvents
	return func(callID string, cursor uint64, limit int) ([]rpc.VoiceCallEvent, bool, bool) {
		events, truncated, found := ring.Since(callID, cursor, limit)
		out := make([]rpc.VoiceCallEvent, 0, len(events))
		for _, ev := range events {
			out = append(out, rpc.VoiceCallEvent{
				Seq:    ev.Seq,
				Kind:   ev.Kind,
				At:     ev.At.Format(time.RFC3339Nano),
				Text:   ev.Text,
				Words:  ev.Words,
				Ms:     ev.Ms,
				Reason: ev.Reason,
			})
		}
		return out, truncated, found
	}
}

// statusTargetDisplay renders one registry row's disclosure-shaped target
// pair (contact-display leaf 02 §4, O1/O4). The phone leg resolves the
// E.164 through the boundary resolver (name + last-4 display; raw number
// NEVER reaches the wire). The local leg's "local:<label>" target carries
// no number, so it passes through verbatim as both fields — clients get a
// single uniform display pair per row.
func statusTargetDisplay(w *VoiceWiring, legName, target string) (string, string) {
	if legName != voice.LegPhone {
		return target, target
	}
	info := w.resolveDisplay(context.Background(), target)
	return info.Name, info.Display
}

// localEmployeeFor resolves the employee id for a live local call from
// the registry snapshot (the leg's calls map is private; the registry
// row keyed by the same ccid carries the employee). Unknown -> "-".
func localEmployeeFor(registry voice.Registry, ccid string) string {
	if registry == nil {
		return "-"
	}
	for _, inst := range registry.Live() {
		if inst.CallControlID == ccid {
			return inst.EmployeeID
		}
	}
	return "-"
}

// mapVoiceLocalOpenErr maps the leg's named refusals onto the RPC
// surface's named errors (CL8). Unknown errors pass through wrapped.
func mapVoiceLocalOpenErr(err error) error {
	switch {
	case errors.Is(err, voice.ErrLocalDisabled):
		return fmt.Errorf("%w", rpc.ErrVoiceLocalDisabled)
	case errors.Is(err, voice.ErrNoEmployee):
		return fmt.Errorf("%w", rpc.ErrVoiceLocalUnknownEmployee)
	case errors.Is(err, voice.ErrBusy):
		return fmt.Errorf("%w: %w", rpc.ErrVoiceLocalBusy, err)
	case errors.Is(err, voice.ErrDuplicateTarget):
		return fmt.Errorf("%w: %w", rpc.ErrVoiceLocalDuplicate, err)
	default:
		return err
	}
}

// registerVoiceLocalRPC registers the voice.local.* surface on the RPC
// server. Always registers (nil wiring = disabled envelope) so clients
// get the named refusal, never "method not found". Pattern:
// registerIntakeStatusRPC (a wiring helper called from daemon New
// before the listeners serve).
func registerVoiceLocalRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	var w *VoiceWiring
	if c != nil {
		w = c.Voice
	}
	seams := voiceLocalSeams(w, employeeManagerFor(c), logger)
	handler := rpc.NewVoiceLocalHandler(seams)
	handler.RegisterVoiceLocalMethods(server)
	if w != nil && w.Local != nil {
		logger.Info("voice.local RPC handlers registered")
	} else {
		logger.Info("voice.local RPC handlers registered (disabled leg: opens refuse)")
	}
}

// employeeManagerFor extracts the employee manager nil-safely.
func employeeManagerFor(c *Components) *employee.Manager {
	if c == nil {
		return nil
	}
	return c.EmployeeManager
}
