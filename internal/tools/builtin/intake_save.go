package builtin

// intake_save (leaf 05 T2 tool surface, secretary guided intake): the
// model-facing wrapper over internal/voiceintake.SaveIntake. The
// voiceintake core enforces the confirm-flag gate; THIS wrapper owns the
// only flag-setting path and the per-call confirm state.
//
// Confirm-flag semantics (ported from the uptractor coordinator rule
// "commit_record is refused unless the caller has had a turn since the
// readback"): the wrapper records the save demand on every Execute; the
// daemon's voice turn seam advances the state machine through
// AdvanceIntakeTurn at each caller-turn boundary. A save files only
// after TWO full caller turns follow the first save demand — the model
// had a turn to speak the read-back, and the caller had the turn that
// answers it. No tool argument can set the flag.
//
// Registry name "intake_save" (leaf 05 contract); tool action maps onto
// the existing ledger_write row (RiskLow, same policy family as
// ledger_append). tools_allowed still governs execution through the
// EXISTING constitution path (internal/employee/enforcement.go 1b) — the
// secretary's definition carries the grant only when guided intake is on.

import (
	"context"
	"sort"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/voiceintake"
)

// IntakeSaveTool is the guided-intake write tool.
type IntakeSaveTool struct {
	tools.ToolDefaults
	// saver is the static ledger seam (simple wirings, tests).
	saver voiceintake.IntakeSaver
	// filerFor builds the PER-CALL filer from the turn context when the
	// attribution (employee id + ccid) only exists there (daemon
	// wiring). When nil, saver is used.
	filerFor func(ctx context.Context) voiceintake.IntakeSaver
	// callID resolves the ccid of the calling voice turn (the wiring
	// binds it from the per-call stamp; nil fails closed — intake_save
	// is a guided-intake tool and only exists on calls).
	callID func(ctx context.Context) string
}

// NewIntakeSaveTool constructs the tool over the daemon's ledger seam.
// A nil saver constructs fine (every call refuses) so wiring can register
// before the ledger store resolves.
func NewIntakeSaveTool(saver voiceintake.IntakeSaver) *IntakeSaveTool {
	return &IntakeSaveTool{saver: saver}
}

// SetFilerFunc arms the per-call filer seam (daemon wiring: the ledger
// append needs the employee id + ccid that only exist on the turn
// context). Nil fn is ignored.
func (t *IntakeSaveTool) SetFilerFunc(fn func(ctx context.Context) voiceintake.IntakeSaver) {
	if fn == nil {
		return
	}
	t.filerFor = fn
}

// SetCallIDFunc arms the ccid seam (AgentIDFunc pattern, voice_call
// precedent). Nil fn is ignored.
func (t *IntakeSaveTool) SetCallIDFunc(fn func(ctx context.Context) string) {
	if fn == nil {
		return
	}
	t.callID = fn
}

func (t *IntakeSaveTool) Name() string { return "intake_save" }

func (t *IntakeSaveTool) Category() string { return "platform" }

func (t *IntakeSaveTool) Description() string {
	return "Save the confirmed intake record to your ledger. Use ONLY after you said the read-back word for word and the caller then confirmed it in their reply. Arguments: kind (the record type, e.g. message or change_request) and fields (each collected field name -> value). If the caller has not confirmed yet, the save is refused and nothing is written — say the read-back and wait instead."
}

func (t *IntakeSaveTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"kind": {
				Type:        "string",
				Description: "The record kind — the intake vocabulary word (e.g. message, change_request). Required.",
			},
			"fields": {
				Type:        "object",
				Description: "The collected intake fields as name -> value pairs, in the caller's words (e.g. {\"caller_name\": \"Dana\", \"message\": \"call the customer\"}).",
			},
		},
		Required: []string{"kind", "fields"},
	}
}

// ---- per-call confirm state (the flag the gate reads) ----
//
// State machine per ccid (ported from the uptractor coordinator's
// turn-count rule verbatim: a commit is refused while it happens in the
// SAME turn as the proposal — p.turn == s.turn — and allowed once a full
// caller turn separates them):
//
//	attempted --(one caller turn boundary)--> confirmed
//
// attempted is set by Execute ITSELF on every pre-confirmation
// intake_save call (its refusal text is the instruction to speak the
// read-back); the daemon's voice turn seam advances the state machine
// through AdvanceIntakeTurn at each caller-turn boundary. A save files
// only in the confirmed state — reachable ONLY after a full caller turn
// has passed since the first save demand, so the model had a turn to
// speak the read-back and the caller had the turn that answers it. The
// flag is CODE, not prompt hope: no tool argument influences it, and a
// fresh save cycle after a successful save starts the countdown over.

type intakeSessionState struct {
	mu        sync.Mutex
	attempted map[string]bool // a save was demanded this turn (read-back owed)
	confirmed map[string]bool // a caller turn completed after the demand
}

var intakeSessions = &intakeSessionState{
	attempted: make(map[string]bool),
	confirmed: make(map[string]bool),
}

// AdvanceIntakeTurn is the voice turn seam: the daemon calls it at each
// CALLER turn boundary (before the model runs on the new caller
// utterance). It promotes the per-call confirm state machine; the daemon
// keeps no intake state of its own.
func AdvanceIntakeTurn(ccid string) {
	if ccid == "" {
		return
	}
	intakeSessions.mu.Lock()
	if intakeSessions.attempted[ccid] {
		intakeSessions.attempted[ccid] = false
		intakeSessions.confirmed[ccid] = true
	}
	intakeSessions.mu.Unlock()
}

// ClearIntakeConfirmed drops ccid's intake state (call teardown, release
// hook).
func ClearIntakeConfirmed(ccid string) {
	if ccid == "" {
		return
	}
	intakeSessions.mu.Lock()
	delete(intakeSessions.attempted, ccid)
	delete(intakeSessions.confirmed, ccid)
	intakeSessions.mu.Unlock()
}

func (t *IntakeSaveTool) confirmed(ccid string) bool {
	intakeSessions.mu.Lock()
	defer intakeSessions.mu.Unlock()
	return intakeSessions.confirmed[ccid]
}

// resetIntakeSession starts a fresh confirm cycle for ccid after a
// successful save: the NEXT record needs its own read-back + confirmation.
func resetIntakeSession(ccid string) {
	intakeSessions.mu.Lock()
	delete(intakeSessions.attempted, ccid)
	delete(intakeSessions.confirmed, ccid)
	intakeSessions.mu.Unlock()
}

func (t *IntakeSaveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.saver == nil && t.filerFor == nil {
		return tools.NewErrorResult("intake_save refused: no ledger filer wired"), nil
	}
	ccid := ""
	if t.callID != nil {
		ccid = t.callID(ctx)
	}
	if ccid == "" {
		return tools.NewErrorResult("intake_save refused: no call on this turn — intake_save is a guided-intake (voice call) tool"), nil
	}
	// Record the save demand BEFORE the gate check: this call is what
	// puts the read-back into the conversation (the refusal text tells
	// the model to speak it), so the confirmation countdown starts here.
	intakeSessions.mu.Lock()
	intakeSessions.attempted[ccid] = true
	intakeSessions.mu.Unlock()

	if !t.confirmed(ccid) {
		return tools.NewErrorResult("intake_save refused: the caller has not confirmed the read-back yet. Say the read-back, then wait for their confirmation"), nil
	}

	kind, _ := args["kind"].(string)
	fieldsRaw, _ := args["fields"].(map[string]any)
	fields := make(map[string]string, len(fieldsRaw))
	for k, v := range fieldsRaw {
		if s, ok := v.(string); ok {
			fields[k] = s
		}
	}

	rec := voiceintake.IntakeRecord{Kind: kind, Fields: fields}
	// confirm=true here is the defense-in-depth leg: the flag was
	// already checked above; SaveIntake's own gate is the pinned core.
	if err := voiceintake.SaveIntake(t.filerFor(ctx), rec, true); err != nil {
		return tools.NewErrorResult("intake_save refused: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}
	// One save per confirmation cycle: the next record demands a fresh
	// read-back + caller turn (attempted/confirmed cleared).
	resetIntakeSession(ccid)
	return map[string]any{
		"saved":   true,
		"kind":    kind,
		"subject": rec.Subject(),
	}, nil
}

// NormalizeIntakeFields returns the collected field names sorted (the
// deterministic read-back order contract).
func NormalizeIntakeFields(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// ---- package-level seams the daemon arms before provider construction ----
//
// The voice provider folds these into every intake_save tool it builds
// (voiceProvider.Discover), mirroring the voiceLocalFunc/contactChainFunc
// pattern: the daemon holds the attribution context, the builtin carries
// plain closures.

var (
	// intakeCallIDFunc resolves the calling turn's ccid.
	intakeCallIDFunc func(ctx context.Context) string
	// intakeFilerFunc builds the per-call filer (employee id + ccid ride
	// the turn context stamps; the adapter lives in the daemon).
	intakeFilerFunc func(ctx context.Context) voiceintake.IntakeSaver
)

// SetIntakeCallIDFunc arms the ccid seam. Nil fn is ignored.
func SetIntakeCallIDFunc(fn func(ctx context.Context) string) {
	if fn == nil {
		return
	}
	intakeCallIDFunc = fn
}

// SetIntakeFilerFunc arms the per-call filer seam. Nil fn is ignored.
func SetIntakeFilerFunc(fn func(ctx context.Context) voiceintake.IntakeSaver) {
	if fn == nil {
		return
	}
	intakeFilerFunc = fn
}

// foldIntakeSeams copies the package-level seams onto one constructed
// tool (provider Discover path).
func foldIntakeSeams(t *IntakeSaveTool) {
	t.SetCallIDFunc(intakeCallIDFunc)
	t.SetFilerFunc(intakeFilerFunc)
}
