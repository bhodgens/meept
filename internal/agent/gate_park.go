package agent

// Gate-park checkpointing (hitl-notifications leaf 02, contract C5).
//
// A human gate pause (a plan sitting in pending_approval, or a goal
// waiting on operator signoff) is checkpointed through the EXISTING
// TurnParker with a new ParkKindGate scope: the pending gate plus the
// turn's re-entry data persist via the parker's kind-scoped
// SetParkPersistence rows, so the approval may arrive out-of-band
// (Slack, CLI, GUI) or after a daemon restart, and the resume
// re-enqueues the turn exactly once.
//
// SINGLE PARK PATH: no second park machinery is introduced. Gate parks
// ride TurnParker.Park with Class=FailureNone and a far-future ResumeAt
// (gateParkHorizon), so the parker's time-based drain never
// auto-approves a gate -- resumption happens ONLY through ResumeByKey,
// which the approval paths (daemon gate-resume wiring) invoke on the
// plan.approved / agents.goals.approve choke points. All of the
// parker's semantics are preserved untouched:
//   - persistence rows keyed by (ParkKindGate, gate key) -- delete
//     BEFORE the resume callback (at-most-once resume: a crash
//     mid-resume can never double-deliver the re-enqueue);
//   - Start re-arms surviving rows after a restart (gate parks have no
//     timeout today: a gate waiting for approval simply survives until
//     approved -- the parker's expiry semantics map nothing, and gate
//     parks are deliberately never auto-expired).
//
// This file owns nothing the Checkpoint struct (retry_recovery.go)
// touches: Checkpoint is crash-recovery ONLY and is untouched.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// gateParkHorizon is the scheduled resume time stamped on every gate
// park. A gate has no provider-derived deadline; the parker requires a
// future ResumeAt, so gate parks schedule ten years out. The drain can
// never legitimately reach this time on a running daemon, and even a
// pathological boot re-arm deletes the row before invoking the resume
// callback (at-most-once), so the horizon value is invisible in normal
// operation. Constant (not config) so a gate can never be quietly
// auto-approved by a misconfigured schedule.
const gateParkHorizon = 10 * 365 * 24 * time.Hour

// GateParkData is the checkpointed gate payload (C5, frozen field set).
// It is the TurnPayload of a ParkKindGate park row: GateKind selects the
// resume route, GateID identifies the pending gate, and TurnReentry
// carries whatever turn re-entry data the pausing surface needs back.
type GateParkData struct {
	GateKind    string         `json:"gate_kind"`    // "plan" | "goal"
	GateID      string         `json:"gate_id"`      // plan id / goal id
	SessionID   string         `json:"session_id"`   // owning session ("" = session-less)
	TurnReentry map[string]any `json:"turn_reentry"` // turn re-entry payload
	RequestedAt time.Time      `json:"requested_at"` // when the gate was raised
}

// gateParkKey derives the persistence key for a gate park: the C5
// identity is (GateKind, GateID) -- re-raising the same gate overwrites
// its own row instead of accumulating duplicates, and the approval path
// resolves the row by exactly this identity. Sessionless goal gates
// share the same namespace (goal ids are unique already).
func gateParkKey(data GateParkData) string {
	return data.GateKind + "|" + data.GateID
}

// gateTurnToRecord encodes a GateParkData as a Class=FailureNone
// ParkedTurnRecord for the shared parker. Class=FailureNone marks the
// record as a gate checkpoint (not a provider wait) end to end: the
// wire class round-trips as "none", and no quota/throttle resume router
// can misinterpret it.
func gateTurnToRecord(data GateParkData) (ParkedTurnRecord, error) {
	if data.GateKind == "" || data.GateID == "" {
		return ParkedTurnRecord{}, fmt.Errorf("gate park: gate_kind and gate_id are required")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ParkedTurnRecord{}, fmt.Errorf("gate park: payload encode: %w", err)
	}
	return ParkedTurnRecord{
		ConversationID: data.SessionID,
		SessionID:      data.SessionID,
		AgentID:        "gate",
		Class:          llm.FailureNone,
		ResumeAt:       time.Now().UTC().Add(gateParkHorizon),
		TurnPayload:    raw,
	}, nil
}

// recordToGateTurn decodes a ParkKindGate row back into its GateParkData
// plus the persistence key the approval path resolves the row by.
func recordToGateTurn(rec ParkedTurnRecord) (GateParkData, string, error) {
	var data GateParkData
	if err := json.Unmarshal(rec.TurnPayload, &data); err != nil {
		return GateParkData{}, "", fmt.Errorf("gate park: payload decode: %w", err)
	}
	return data, gateParkKey(data), nil
}

// DecodeGatePark is the exported decode for consumers outside package
// agent (the daemon's gate wiring re-park fallback); it returns the
// checkpoint payload without the persistence key.
func DecodeGatePark(rec ParkedTurnRecord) (GateParkData, string, error) {
	return recordToGateTurn(rec)
}

// GateParker checkpoints human-gate pauses through ONE shared
// TurnParker (ParkKindGate scope). It is constructed by the daemon's
// gate-resume wiring beside the other parkers; a nil parker or store
// degrades to disabled parking exactly like the other park sites.
//
// resume is invoked (in a background goroutine) at most once per gate
// park, after the row has been deleted from persistence -- the existing
// TurnParker delete-before-callback order, unchanged.
type GateParker struct {
	logger *slog.Logger
	turns  *TurnParker
}

// NewGateTurnParker constructs the DEDICATED gate TurnParker (one
// parker per ParkKind, the established layout) with MaxWait sized to
// cover the gate horizon: the parker's MaxWait soft-stop rewrites any
// longer wait to now+MaxWait, so a shared/chat-sized MaxWait would
// silently turn a 10-year gate schedule into a short timer. Poll
// cadence stays the parker default (gate parks drain only via
// ResumeByKey anyway).
func NewGateTurnParker(logger *slog.Logger, resume func(context.Context, ParkedTurnRecord)) *TurnParker {
	return NewTurnParker(logger, resume, 2*gateParkHorizon)
}

// NewGateParker wraps an existing TurnParker for gate checkpoints. The
// parker must be DEDICATED to gate parks (ParkKindGate scoping via
// SetParkPersistence happens in the daemon wiring, before Start) -- the
// same one-parker-per-kind layout the chat/throttle/episode parkers
// already use, sharing the one parked_turns table. Construction
// installs the gate persistence-key override ((gate_kind|gate_id)
// derived from the payload), so a re-raised gate overwrites its own row
// and ResumeByKey resolves rows by the C5 identity.
func NewGateParker(logger *slog.Logger, turns *TurnParker) *GateParker {
	if logger == nil {
		logger = slog.Default()
	}
	if turns != nil {
		turns.SetParkKey(func(rec ParkedTurnRecord) string {
			// Lenient decode mirrors the episode wiring: a malformed
			// payload still persists under an empty-identity fallback
			// key instead of being dropped unpersisted.
			var data GateParkData
			if err := json.Unmarshal(rec.TurnPayload, &data); err != nil {
				return "|"
			}
			return gateParkKey(data)
		})
	}
	return &GateParker{logger: logger.With("component", "gate-parker"), turns: turns}
}

// Turns exposes the underlying TurnParker (the daemon's Start call and
// persistence wiring target it directly; the gate parker adds no
// lifecycle of its own).
func (p *GateParker) Turns() *TurnParker { return p.turns }

// ParkGate checkpoints one gate pause. It returns false (nothing
// parked) when parking is disabled, the payload cannot be encoded, or
// the shared parker refuses the record. A re-raise of an already-parked
// gate overwrites the row (same (kind, park_key)) and re-queues the
// in-memory record -- the approval still resumes exactly once, because
// ResumeByKey pops one queued record per key.
func (p *GateParker) ParkGate(data GateParkData) bool {
	if p == nil || p.turns == nil {
		return false
	}
	rec, err := gateTurnToRecord(data)
	if err != nil {
		p.logger.Warn("gate park refused", "gate_kind", data.GateKind, "gate_id", data.GateID, "error", err)
		return false
	}
	if !p.turns.Park(rec) {
		return false
	}
	p.logger.Info("gate parked pending approval",
		"gate_kind", data.GateKind,
		"gate_id", data.GateID,
		"session_id", data.SessionID,
	)
	return true
}

// ResumeByKey resumes the gate identified by gateKind+gateID, if one is
// parked. The persistence row is deleted BEFORE the resume callback
// runs (the parker's at-most-once order, unpersistPark + drainDue), so
// a duplicate or racing approval finds nothing and is a no-op. Returns
// false when no matching gate is parked.
func (p *GateParker) ResumeByKey(ctx context.Context, gateKind, gateID string) bool {
	if p == nil || p.turns == nil {
		return false
	}
	return p.turns.resumeByKey(ctx, ParkKindGate, gateKind+"|"+gateID)
}

// PendingGates returns the number of currently parked gate rows
// (observability surface; mirrors TurnParker.Pending).
func (p *GateParker) PendingGates() int {
	if p == nil {
		return 0
	}
	return p.turns.Pending()
}

// resumeByKey resumes one parked record by its kind-scoped persistence
// key, bypassing the time-based drain. It is the mechanism the gate
// approval path uses (an approval is an event, not a schedule): the
// persistence row is deleted BEFORE the resume callback runs -- the
// parker's existing at-most-once order, exactly as drainDue does -- so a
// duplicate or racing approval finds nothing and returns false.
//
// Additive seam (hitl-notifications leaf 02): no core parker behavior
// changes. Park/Resume/persist internals are untouched; this reads the
// same mutex-guarded queue the drain reads and reuses unpersistPark.
func (p *TurnParker) resumeByKey(ctx context.Context, kind ParkKind, key string) bool {
	if p == nil || key == "" {
		return false
	}
	p.mu.Lock()
	resume := p.resumeFunc
	idx := -1
	for i, k := range p.parkKeys {
		if k == key {
			idx = i
			break
		}
	}
	if idx < 0 || resume == nil {
		p.mu.Unlock()
		return false
	}
	rec := p.parked[idx]
	p.parked = append(p.parked[:idx], p.parked[idx+1:]...)
	p.parkKeys = append(p.parkKeys[:idx], p.parkKeys[idx+1:]...)
	p.mu.Unlock()

	// Delete-before-callback (at-most-once), mirroring drainDue.
	p.unpersistPark(key)
	p.logger.Info("resuming parked record by key",
		"kind", string(kind),
		"session_id", rec.SessionID,
	)
	p.wg.Add(1)
	func() {
		defer p.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				p.logger.Error("resume callback panicked -- record dropped",
					"class", rec.Class,
					"session_id", rec.SessionID,
					"panic", r,
				)
			}
		}()
		resume(ctx, rec)
	}()
	return true
}
