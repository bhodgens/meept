// Package gatecapture captures human gate verdicts paired with the model's
// prior command-safety scores (jev-command-gate leaf 02,
// docs/plans/20261031-jev-command-gate/02-capture-export.md, Contract C4).
//
// TWO CHOKEPOINTS, EXACTLY:
//
//  1. "decision_gate_deny" - a deny escalated to confirmation is resolved by
//     the human (the confirmed re-invoke passing
//     internal/tools/builtin/decision_gate.go). The deny itself only ARMS a
//     pending capture; the record lands at the resolution.
//  2. "permission_table_ask" - a shell permission-table ask routed to the
//     confirmation flow (internal/security engine stage 1.5). The ask arms a
//     pending capture (memory-only, safe under the engine's read lock); the
//     resolution lands the record.
//
// A record stores: command text, the serialized C2 context bytes
// (jevgate.SerializeCommandContext, verbatim), the prior score/confidence and
// degraded flag from the jevgate score gate when it produced one for the
// command (the documented zero-value otherwise), the human decision
// ("allowed" | "denied"), and the Approver identity from the confirmation
// flow's context. When the flow carries no identity the documented EMPTY
// approver is stored - never a guessed name.
//
// SECRETS TWO-LANE SPIRIT: records never contain secret values - no env
// values, no credential material, no file contents beyond the command text
// itself. The ContextBlob is command + cwd + session facts only, and the
// pending-capture pipeline never accepts env or secret inputs.
//
// Nil-tolerance: every sink method is safe on a nil *Store and on a store
// whose sqlite half failed to open (pending state still tracks in memory;
// record writes degrade to a logged miss). A capture failure NEVER blocks or
// fails a gate or confirmation flow.
package gatecapture

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	sid "github.com/caimlas/meept/pkg/id"
)

// Chokepoint identifiers (Contract C4, frozen vocabulary - exactly two).
const (
	ChokepointDecisionGateDeny   = "decision_gate_deny"
	ChokepointPermissionTableAsk = "permission_table_ask"
)

// HumanDecision vocabulary (Contract C4, frozen).
const (
	DecisionAllowed = "allowed"
	DecisionDenied  = "denied"
)

// CommandGateRecord is one human gate verdict paired with the model's prior
// score for the same command (Contract C4).
type CommandGateRecord struct {
	// ID is pkg/id.Generate-derived (never time.Now/rand). Empty on Record
	// input: the store mints one.
	ID string
	// Ts is the resolution timestamp (UTC). Zero on Record input: the store
	// stamps it.
	Ts time.Time
	// Chokepoint is one of the two Chokepoint* constants above.
	Chokepoint string
	// Command is the command text (or bounded args summary when the tool
	// call carried no "command" argument).
	Command string
	// ContextBlob is the C2 serialization, verbatim bytes.
	ContextBlob string
	// PriorScore / PriorConfidence / PriorDegraded mirror
	// llm.CommandScoreResult when the jevgate score gate produced a result
	// for this command; otherwise the documented zero-value
	// (0.0 / 0.0 / false) - which only ever happens together with
	// hasPrior=false in the pending state, never fabricated in a row.
	PriorScore      float64
	PriorConfidence float64
	PriorDegraded   bool
	// HumanDecision is "allowed" or "denied".
	HumanDecision string
	// Approver is the confirmation flow's rendered identity
	// (acp.Approver.String()) or "" when the flow carried none.
	Approver string
}

// ListOptions filters ListRecords.
type ListOptions struct {
	// Chokepoint filters to one chokepoint; "" lists both.
	Chokepoint string
	// Since filters to records at or after the instant; nil lists all.
	Since *time.Time
	// Limit caps the row count (0 = no cap); Offset skips rows.
	Limit  int
	Offset int
}

const (
	// maxPending bounds the in-memory pending-capture map. Eviction is
	// oldest-timestamp-first; a pending entry is one gate escalation, so
	// the steady-state size is tiny.
	maxPending = 256
	// pendingTTL drops stale pendings lazily on insert: an escalation whose
	// resolution never landed within the TTL is dead state.
	pendingTTL = time.Hour
	// approverEmpty is the documented empty approver: stored when the
	// confirmation flow carries no identity, never a guessed name.
	approverEmpty = ""
)

// pendingGate is the armed-but-unresolved capture: the command context and
// prior score fields captured at escalation time, waiting for the human
// resolution to land the row.
type pendingGate struct {
	command         string
	contextBlob     string
	hasPrior        bool
	priorScore      float64
	priorConfidence float64
	priorDegraded   bool
	ts              time.Time
}

// Store is the capture store: the sqlite row store (store_sqlite.go) plus
// the bounded in-memory pending map that pairs escalation-time prior scores
// with resolution-time human verdicts.
//
// It satisfies jevgate.Recorder (the leaf-01 nil-tolerant capture seam on
// ScoreGateHook) and the two chokepoint sink interfaces defined beside the
// chokepoints (internal/tools/builtin decision_gate.go and internal/security
// engine.go) - all nil-tolerant on the receiver.
type Store struct {
	db *gateDB

	mu      sync.Mutex
	pending map[string]*pendingGate
}

// compile-time: the store implements the leaf-01 Recorder seam.
var _ interface {
	RecordScore(ctx context.Context, command string, serializedContext string, result *llmScoreResult, escalated bool, reason string)
} = (*Store)(nil)

// OpenStore opens (and migrates) the capture sqlite DB at path. The path is
// typically DefaultDBPath beside the shadow store (Contract C4).
func OpenStore(path string) (*Store, error) {
	db, err := openGateDB(path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, pending: make(map[string]*pendingGate)}, nil
}

// Close closes the sqlite handle. Pending in-memory state is dropped.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.pending = make(map[string]*pendingGate)
	s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return s.db.close()
}

// Record writes one record, minting ID and Ts when unset. The sqlite write
// happens with NO store mutex held (mutexio).
func (s *Store) Record(ctx context.Context, rec *CommandGateRecord) error {
	if s == nil || rec == nil {
		return nil
	}
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = sid.Generate("jgate")
	}
	if rec.Ts.IsZero() {
		rec.Ts = time.Now().UTC()
	}
	switch rec.Chokepoint {
	case ChokepointDecisionGateDeny, ChokepointPermissionTableAsk:
	default:
		return errInvalidChokepoint(rec.Chokepoint)
	}
	switch rec.HumanDecision {
	case DecisionAllowed, DecisionDenied:
	default:
		return errInvalidDecision(rec.HumanDecision)
	}
	if s.db == nil {
		slog.Debug("gatecapture: no db wired; record dropped",
			"command_len", len(rec.Command), "chokepoint", rec.Chokepoint)
		return nil
	}
	return s.db.insert(ctx, rec)
}

// ListRecords lists records newest-first under the given filters.
func (s *Store) ListRecords(ctx context.Context, opts ListOptions) ([]CommandGateRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	return s.db.list(ctx, opts)
}

// ---- jevgate.Recorder seam (leaf 01 attach point) ----

// RecordScore implements jevgate.Recorder. Only ESCALATIONS arm a pending
// capture (a clean allow never reaches a human chokepoint). The prior score
// fields and the serialized C2 context are stashed keyed by command text so
// the resolution - possibly several turns later, after the confirmation
// flow - can land one record with the prior attached. Nil-tolerant and
// best-effort: a panic-free no-op when the receiver is nil; never an error
// path (the interface is void).
func (s *Store) RecordScore(_ context.Context, command, serializedContext string, result *llmScoreResult, escalated bool, _ string) {
	if s == nil || !escalated {
		return
	}
	p := &pendingGate{
		command:     command,
		contextBlob: serializedContext,
		ts:          time.Now(),
	}
	if result != nil {
		p.hasPrior = true
		p.priorScore = result.Score
		p.priorConfidence = result.Confidence
		p.priorDegraded = result.Degraded
	}
	s.armPending(p)
}

// ---- chokepoint 1: decision_gate deny -> human resolves ----

// DecisionGateDeny arms the chokepoint-1 pending capture for a deny
// escalated to confirmation (called from decision_gate.go's deny branch).
// Merges with an existing pending (the score gate may have stashed the
// prior earlier in the same attempt). Memory-only.
func (s *Store) DecisionGateDeny(_ context.Context, command string) {
	s.armPendingFor(command, ChokepointDecisionGateDeny)
}

// DecisionGateResolved lands the chokepoint-1 record: the human resolved
// the deny escalated to confirmation (called from decision_gate.go's
// confirmed re-invoke - the observable human approval at this seam).
// decision must be "allowed" or "denied"; anything else is dropped (no
// fabricated verdicts). approver is the rendered identity or "" when the
// flow carried none.
func (s *Store) DecisionGateResolved(_ context.Context, command, decision, approver string) {
	s.resolve(ChokepointDecisionGateDeny, command, decision, approver)
}

// ---- chokepoint 2: permission-table ask -> confirmation resolution ----

// PermissionTableAsk arms the chokepoint-2 pending capture for a shell
// permission-table ask routed to the confirmation flow (called from the
// security engine's stage-1.5 ask branch). Memory-ONLY by design: the engine
// holds a read lock across the check, so this must never touch sqlite
// (mutexio). Safe on a nil receiver.
func (s *Store) PermissionTableAsk(command string) {
	s.armPendingFor(command, ChokepointPermissionTableAsk)
}

// PermissionTableResolved lands the chokepoint-2 record: the permission-table
// ask was answered by the confirmation flow. The gateway wrap asserts the
// daemon-side armed-ask mark FIRST (set together with PermissionTableAsk by
// the wiring adapter) so an unarmed resolution - store restarted, ask raced
// an eviction, or a decision-gate approval - never fabricates a context.
// Same decision vocabulary and no-fabrication rule as DecisionGateResolved.
func (s *Store) PermissionTableResolved(_ context.Context, command, decision, approver string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	p := s.popPendingLocked(command)
	s.mu.Unlock()
	if p == nil {
		slog.Debug("gatecapture: ask resolution without an armed ask; dropped",
			"chokepoint", ChokepointPermissionTableAsk)
		return
	}
	s.land(ChokepointPermissionTableAsk, p, decision, approver)
}

// ---- internals ----

// armPendingFor arms (or refreshes) the pending for command without
// disturbing an already-stashed prior.
func (s *Store) armPendingFor(command, _ string) {
	if s == nil || strings.TrimSpace(command) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = make(map[string]*pendingGate)
	}
	if p, ok := s.pending[command]; ok {
		p.ts = time.Now() // refresh; keep the prior
		return
	}
	s.insertPendingLocked(&pendingGate{command: command, ts: time.Now()})
}

// armPending inserts p, evicting the oldest entry past maxPending and
// dropping TTL-expired state.
func (s *Store) armPending(p *pendingGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = make(map[string]*pendingGate)
	}
	if old, ok := s.pending[p.command]; ok {
		// A prior already stashed for this command wins only if the new
		// escalation carries one and the old did not (re-asks re-arm).
		if !old.hasPrior && p.hasPrior {
			old.hasPrior = p.hasPrior
			old.priorScore = p.priorScore
			old.priorConfidence = p.priorConfidence
			old.priorDegraded = p.priorDegraded
			old.contextBlob = p.contextBlob
		}
		old.ts = time.Now()
		return
	}
	s.insertPendingLocked(p)
}

// insertPendingLocked inserts and bounds the map. Caller holds s.mu.
func (s *Store) insertPendingLocked(p *pendingGate) {
	now := time.Now()
	for k, v := range s.pending {
		if now.Sub(v.ts) > pendingTTL {
			delete(s.pending, k)
		}
	}
	if len(s.pending) >= maxPending {
		var oldestKey string
		var oldest time.Time
		first := true
		for k, v := range s.pending {
			if first || v.ts.Before(oldest) {
				oldestKey, oldest, first = k, v.ts, false
			}
		}
		if oldestKey != "" {
			delete(s.pending, oldestKey)
		}
	}
	s.pending[p.command] = p
}

// popPending removes and returns the pending for command. Caller holds s.mu.
func (s *Store) popPendingLocked(command string) *pendingGate {
	p, ok := s.pending[command]
	if !ok {
		return nil
	}
	delete(s.pending, command)
	return p
}

// resolve lands the record for a chokepoint resolution (chokepoint 1). The
// pending is consumed when present; a lost pending (restart, eviction)
// still lands the record with the bare-command C2 serialization and the
// documented zero-value prior - never a guessed context.
func (s *Store) resolve(chokepoint, command, decision, approver string) {
	if s == nil {
		return
	}
	switch decision {
	case DecisionAllowed, DecisionDenied:
	default:
		slog.Debug("gatecapture: resolution without a verdict vocabulary decision; dropped",
			"chokepoint", chokepoint, "decision", decision)
		return
	}
	s.mu.Lock()
	p := s.popPendingLocked(command)
	s.mu.Unlock()
	s.land(chokepoint, p, decision, approver)
}

// land writes the record from a (possibly nil) pending. No store mutex held
// across the sqlite write (mutexio).
func (s *Store) land(chokepoint string, p *pendingGate, decision, approver string) {
	command := ""
	if p != nil {
		command = p.command
	}
	rec := &CommandGateRecord{
		Chokepoint:    chokepoint,
		Command:       command,
		ContextBlob:   serializeBare(command),
		HumanDecision: decision,
		Approver:      approver,
	}
	if p != nil {
		if p.contextBlob != "" {
			rec.ContextBlob = p.contextBlob
		}
		rec.PriorScore = p.priorScore
		rec.PriorConfidence = p.priorConfidence
		rec.PriorDegraded = p.priorDegraded
	}
	if err := s.Record(context.Background(), rec); err != nil {
		// Capture NEVER fails the flow that produced the verdict.
		slog.Debug("gatecapture: record write failed", "error", err)
	}
}

// serializeBare renders the C2 serialization of a bare command (empty cwd,
// empty facts) - the documented fallback ContextBlob.
func serializeBare(command string) string {
	return serializeContext(command, "", nil)
}
