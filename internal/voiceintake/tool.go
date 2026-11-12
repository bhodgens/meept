package voiceintake

// intake_save tool core (leaf 05 T2): the confirmed-write path of guided
// intake. The caller-facing tool wrapper lives in internal/tools/builtin
// (intake_save.go) — this file holds the package-level core so the gate
// tests can pin the CONFIRM-FLAG RULE directly:
//
//	a save with confirm=false is REFUSED at this layer and files NOTHING;
//	a save with confirm=true files exactly one record (kind per the
//	IntakeSpec vocabulary, subject = the collected record text).
//
// The gate is CODE, not prompt: the confirm flag arrives from the tool
// wrapper, which sets it ONLY after an explicit caller confirmation turn
// (read-back spoken, caller answered confirm). A model that hallucinates
// "the caller said confirm" cannot reach this file with confirm=true —
// the wrapper's turn-tracking owns the flag (see
// internal/tools/builtin/intake_save.go).
//
// Ledger posture mirrors leaf 04's CommitmentFiler seam: internal/
// voiceintake must not import internal/ledger (existing repo posture),
// so the append rides the IntakeFiler interface and the real
// ledger.Store.Append adapter (kind per spec Kind field, source=voice,
// source_ref=ccid) lives in internal/daemon wiring — wiring lands with
// the daemon-side guided-intake enablement.

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRefusedWithoutConfirm is the refusal the gate returns when a save
// arrives with no prior explicit confirmation turn.
var ErrRefusedWithoutConfirm = errors.New("intake_save refused: no confirmed read-back turn — read the record back and wait for the caller's explicit confirmation first")

// ErrMissingKind is the schema refusal for a record without a Kind (the
// kind becomes the ledger subject vocabulary and must be present).
var ErrMissingKind = errors.New("intake_save refused: record kind is required (e.g. message, change_request)")

// ErrMissingFields is the schema refusal for a record with no collected
// fields at all — nothing was taken, nothing can file.
var ErrMissingFields = errors.New("intake_save refused: record has no collected fields")

// IntakeRecord is one confirmed intake payload (schema-validated before
// the filer sees it). Kind is open vocabulary by design (master note:
// the dispatch-board tree later consumes intake records; a string keeps
// the board able to extend kinds without touching this package). Fields
// holds the collected field name -> value pairs exactly as prompted.
type IntakeRecord struct {
	Kind   string
	Fields map[string]string
}

// Subject renders the record's ledger subject: the collected field values
// joined in a stable field-name order, so "call the customer" files as
// that text and the record is speakable back. Deterministic (keys sorted)
// because map iteration is not.
func (r IntakeRecord) Subject() string {
	names := make([]string, 0, len(r.Fields))
	for k := range r.Fields {
		names = append(names, k)
	}
	sortStrings(names)
	vals := make([]string, 0, len(names))
	for _, k := range names {
		if v := strings.TrimSpace(r.Fields[k]); v != "" {
			vals = append(vals, v)
		}
	}
	return strings.Join(vals, " ")
}

// IntakeSaver is the ledger append seam (leaf 04 CommitmentFiler
// pattern). internal/voiceintake must not import internal/ledger (repo
// posture), so the append rides this interface and the real
// ledger.Store.Append adapter lives in internal/daemon wiring, mapping:
//
//	kind   = the record's Kind (per the IntakeSpec vocabulary)
//	source = voice
//	source_ref = the call control id (ccid)
//	subject = record.Subject()
//
// The method set is the gate test's captureSaver shape (save). External
// packages cannot implement an unexported method, so the daemon adapter
// builds on SaverFunc below.
type IntakeSaver interface {
	save(kind, subject string) error
}

// SaverFunc adapts a plain function onto IntakeSaver (defined IN this
// package so it can carry the unexported method; the daemon wiring wraps
// its ledger.Store.Append closure in one of these).
type SaverFunc func(kind, subject string) error

// save implements IntakeSaver.
func (f SaverFunc) save(kind, subject string) error { return f(kind, subject) }

// Validate checks the payload schema: a Kind is required, at least one
// non-blank field is required. Nil otherwise.
func (r IntakeRecord) Validate() error {
	if strings.TrimSpace(r.Kind) == "" {
		return ErrMissingKind
	}
	for _, v := range r.Fields {
		if strings.TrimSpace(v) != "" {
			return nil
		}
	}
	return ErrMissingFields
}

// SaveIntake is the confirm-flag-gated save core: validate the payload,
// then — ONLY when confirm is true, the flag set exclusively by an
// explicit confirmation turn at the tool wrapper — file through the
// seam. A refused save files nothing and returns the refusal error.
func SaveIntake(filer IntakeSaver, rec IntakeRecord, confirm bool) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	if !confirm {
		return ErrRefusedWithoutConfirm
	}
	if filer == nil {
		return errors.New("intake_save refused: no ledger filer wired")
	}
	if err := filer.save(rec.Kind, rec.Subject()); err != nil {
		return fmt.Errorf("intake_save: file: %w", err)
	}
	return nil
}

// sortStrings is a tiny insertion sort (stdlib sort.Strings would pull
// the sort import for three callers; kept local to stay dependency-quiet).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
