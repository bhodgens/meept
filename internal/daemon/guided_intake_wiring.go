package daemon

// Guided-intake daemon wiring (uptractor-ports leaf 05): the ledger
// adapter for voiceintake.IntakeSaver (leaf 04 CommitmentFiler posture —
// internal/voiceintake never imports internal/ledger), the arming rule,
// and the per-call confirm-state hooks the builtin intake_save tool's
// gate rides.
//
// Arming rule (secretary-scoped, owner decision 2026-11-11): the
// intake_save ledger seam is bound ONLY when at least one
// [voice.employee_leg] row carries guided_intake=true — an empty ledger
// store or zero rows keeps the tool unregistered entirely
// (off-by-absence). The per-EMPLOYEE gate is voice.GuidedIntakeFor,
// consulted per call for the prompt extension; execution is finally
// governed by the employee constitution's tools_allowed (enforcement
// 1b), unchanged.

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voiceintake"
)

// guidedIntakePayload is the intake row's payload shape: the record kind
// (the subject carries the collected field text verbatim). The seam
// signature is (kind, subject); the dispatch-board consumers read kind
// from the payload and the collected words from the subject.
type guidedIntakePayload struct {
	Kind   string            `json:"kind"`
	Fields map[string]string `json:"fields"`
}

// ledgerIntakeSaver adapts the employee ledger store onto
// voiceintake.IntakeSaver with the frozen field mapping: one
// ledger.Append(kind=<record kind>, source=voice, source_ref=ccid,
// subject=<collected text>) per confirmed intake record, payload
// carrying the kind + collected fields.
type ledgerIntakeSaver struct {
	store      *ledger.Store
	employeeID string
	ccid       string
}

// save is the append leg (wrapped in voiceintake.SaverFunc at the arming
// site — the IntakeSaver method is unexported, so the adapter rides the
// exported func adapter like every external filer).
func (s *ledgerIntakeSaver) save(kind, subject string) error {
	if s == nil || s.store == nil {
		return errIntakeLedgerUnwired
	}
	payload, err := json.Marshal(guidedIntakePayload{Kind: kind})
	if err != nil {
		return err
	}
	_, err = s.store.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: s.employeeID,
		Kind:       ledger.Kind(kind),
		Subject:    subject,
		Payload:    payload,
		Source:     ledger.SourceVoice,
		SourceRef:  s.ccid,
	})
	return err
}

// errIntakeLedgerUnwired is the fail-closed refusal when the adapter runs
// without a store (never reachable through the arming rule; kept for
// hand-built test wirings).
var errIntakeLedgerUnwired = errIntakeNoStore{}

type errIntakeNoStore struct{}

func (errIntakeNoStore) Error() string { return "intake_save: no ledger store wired" }

// anyGuidedIntakeRow reports whether any [voice.employee_leg] row opts
// in (the tool-EXISTENCE gate — per-employee behavior rides
// voice.GuidedIntakeFor per call).
func anyGuidedIntakeRow(cfg config.VoiceConfig) bool {
	for i := range cfg.EmployeeLeg {
		if cfg.EmployeeLeg[i].GuidedIntake != nil && *cfg.EmployeeLeg[i].GuidedIntake {
			return true
		}
	}
	return false
}

// armIntakeSave binds the builtin intake_save seams over the employee
// ledger when the config opts in. store nil (ledger disabled) or no
// guided_intake row => the tool stays unregistered. Called once from the
// components voice-start closure, before toolprov.RegisterAll.
//
// The ledger append is attributable ONLY through the per-call seam: the
// employee id + ccid ride the turn context stamps (employeeIDContextKey /
// WithIntakeCCID), so intakeFilerFor builds one ledgerIntakeSaver per
// call. The static SetIntakeSaveSaver closure is a fail-closed refusal —
// a provider built without the per-call filer can never misattribute.
func armIntakeSave(store *ledger.Store, cfg config.VoiceConfig, logger *slog.Logger) {
	if store == nil || !anyGuidedIntakeRow(cfg) {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	builtin.SetIntakeSaveSaver(voiceintake.SaverFunc(func(kind, subject string) error {
		return errIntakeLedgerUnwired
	}))
	builtin.SetIntakeCallIDFunc(IntakeCCIDFromContext)
	builtin.SetIntakeFilerFunc(func(ctx context.Context) voiceintake.IntakeSaver {
		employeeID, ccid, ok := intakeTurnAttribution(ctx)
		if !ok {
			return voiceintake.SaverFunc(func(kind, subject string) error {
				return errIntakeLedgerUnwired
			})
		}
		saver := &ledgerIntakeSaver{store: store, employeeID: employeeID, ccid: ccid}
		return voiceintake.SaverFunc(saver.save)
	})
	logger.Info("guided intake armed: intake_save registered (guided_intake employee_leg row)")
}

// intakeCCIDContextKey scopes the calling call-control-id stamp on voice
// turn contexts (dedicated struct key, repo SA1029 pattern). Stamped by
// the daemon turn runner next to the employee/callerid stamps; the
// builtin intake_save tool's callID seam reads it through
// IntakeCCIDFromContext.
type intakeCCIDContextKey struct{}

// WithIntakeCCID stamps the call control id onto a turn context. Empty
// ccid stores nothing.
func WithIntakeCCID(ctx context.Context, ccid string) context.Context {
	if ccid == "" {
		return ctx
	}
	return context.WithValue(ctx, intakeCCIDContextKey{}, ccid)
}

// IntakeCCIDFromContext reads the call control id off a turn context;
// "" when absent (non-voice turn, tests).
func IntakeCCIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(intakeCCIDContextKey{}).(string); ok {
		return v
	}
	return ""
}

// intakeTurnAttribution resolves the (employeeID, ccid) pair a per-call
// filer needs. Both stamps are written by the daemon turn runner.
func intakeTurnAttribution(ctx context.Context) (employeeID, ccid string, ok bool) {
	ccid = IntakeCCIDFromContext(ctx)
	if ccid == "" {
		return "", "", false
	}
	if v, ok := ctx.Value(employeeIDContextKey{}).(string); ok && v != "" {
		return v, ccid, true
	}
	return "", ccid, false
}

// guidedIntakePromptExtension renders the prompt block the voice turn
// runner appends when the bound employee's row opts in: the generic
// one-question-at-a-time procedure (voiceintake.GuidedIntakePrompt).
// Empty string = no extension (gate off).
func guidedIntakePromptExtension(cfg config.VoiceConfig, employeeID string) string {
	if !voice.GuidedIntakeFor(cfg, employeeID) {
		return ""
	}
	return voiceintake.GuidedIntakePrompt
}

// advanceIntakeTurn is the caller-turn-boundary hook: promotes the
// builtin confirm state machine for this call. Called by the daemon turn
// runner at the START of each caller turn. Fail-soft: unknown/absent
// ccids are a no-op inside the builtin.
func advanceIntakeTurn(ccid string) {
	if ccid != "" {
		builtin.AdvanceIntakeTurn(ccid)
	}
}

// clearIntakeSession tears the call's intake state down (release hook).
func clearIntakeSession(ccid string) {
	if ccid != "" {
		builtin.ClearIntakeConfirmed(ccid)
	}
}
