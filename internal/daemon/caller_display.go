package daemon

// caller_display.go — contact-display boundary resolver (plan tree
// docs/plans/20261110-contact-display, leaf 01; gate §1/§2 pinned contract:
// internal/daemon/caller_display_test.go).
//
// DISCLOSURE POLICY (master O1/O2/O3, FROZEN):
//   - Knowing who calls is a feature: the OPERATOR views (Display) carry
//     the contact name plus the last-4 form of the caller number. Only the
//     last four digits ever appear — never a full number.
//   - EGRESS surfaces (bus payloads, push relay) use Egress: the name
//     ONLY, never any digits.
//   - Resolution happens at the daemon boundary (here), never in clients.
//   - Unknown / errored / empty lookups all render "Unknown caller" plus
//     the last-4 tail when the number has digits (O3).
//
// O5 MERGE-ORDER NOTE: contact.LookupByPhone ships with the sibling tree
// plan/caller-authz (20261110-caller-authz) and does NOT exist in this
// tree yet. Per master O5 the resolver is vendored daemon-local behind the
// pinned CallerDisplayLookup interface below, whose signature mirrors
// (*contact.SQLiteStore).LookupByPhone verbatim — when the authz tree
// merges, the store satisfies the seam STRUCTURALLY and the local
// sqliteDisplayLookup shim becomes dead code:
//
//	TODO(caller-authz merge): delete sqliteDisplayLookup and wire
//	*contact.SQLiteStore (which will gain LookupByPhone) directly into
//	VoiceWiring.displayLookup; the interface + resolver stay unchanged.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/contact"
)

// CallerDisplayLookup is the pinned resolver seam (gate header comment):
// it mirrors the authz tree's (*contact.SQLiteStore).LookupByPhone
// signature verbatim. Implementations return (record, found, err); a
// store error must surface as a non-nil err so resolveDisplay can fail
// soft (and WARN) rather than silently render "unknown".
type CallerDisplayLookup interface {
	LookupByPhone(ctx context.Context, e164 string) (*contact.Record, bool, error)
}

// DisplayInfo is the boundary-resolved caller identity (master FROZEN).
type DisplayInfo struct {
	Name    string // contact DisplayName or "Unknown caller"
	Display string // operator-safe: "Jane Doe (•••5567)" / "Unknown caller (•••1234)"
	Egress  string // name only — NEVER digits (master FROZEN egress rule)
}

// NOTE on the pre-commit U1000 (unused-code) gate: the resolver symbols
// below are consumed today by the pinned gate tests (caller_display_test.go
// and voice_ended_display_test.go both set VoiceWiring.displayLookup and
// drive resolveDisplay); the production call sites land with leaf 02's
// surface swaps. The hook runs staticcheck with -tags=test, so that test
// usage keeps the whole resolver live once the gate package compiles again
// (it is currently RED on leaf 02's payload fields) — no suppression
// directive is needed.
const (
	// unknownCallerName is the FROZEN unknown-path label (gate §1/§2).
	unknownCallerName = "Unknown caller"
	// displayMaskPrefix is the last-4 mask lead ("three bullets", U+2022)
	// shared with the gate's "•••NNNN" expectation.
	displayMaskPrefix = "•••"
	// displayResolveTimeout bounds one contact lookup at the boundary
	// (master Interface Contracts: 2s ctx) so a wedged store can never
	// stall a publish surface.
	displayResolveTimeout = 2 * time.Second
)

// resolveDisplay resolves an E.164 caller number into the FROZEN
// DisplayInfo triple at the daemon boundary. Nil-safe end to end: a nil
// wiring, a nil displayLookup seam, a store error, or a not-found all
// take the unknown path. A store error additionally logs at WARN —
// fail-soft must not be silent-lossy (gate §1 pins this).
//
// The lookup is keyed by the CALLER-PASSED e164 verbatim (no
// normalization here — the store-side reads normalize on their own).
func (w *VoiceWiring) resolveDisplay(ctx context.Context, e164 string) DisplayInfo {
	// The identifying tail comes from the number itself, so the unknown
	// path can render it even when the store is down (master: "store
	// error -> render Unknown caller (+last4 from the number itself)").
	tail := lastFourDigits(e164)

	var (
		rec   *contact.Record
		found bool
	)
	if w != nil && w.displayLookup != nil {
		lctx, cancel := context.WithTimeout(ctx, displayResolveTimeout)
		defer cancel()
		var err error
		rec, found, err = w.displayLookup.LookupByPhone(lctx, e164)
		if err != nil {
			// O5 fail-soft: the store error degrades to the unknown
			// render; WARN keeps the degradation observable.
			if w.logger != nil {
				w.logger.Warn("caller display: contact lookup failed, rendering unknown caller",
					"error", err)
			}
			rec, found = nil, false
		} else if found && rec == nil {
			// Defensive: a (mis)implemented seam claiming found with a
			// nil record must not panic the surface.
			found = false
		}
	}

	if found && strings.TrimSpace(rec.DisplayName) != "" {
		name := rec.DisplayName
		return DisplayInfo{
			Name:    name,
			Display: joinDisplay(name, tail),
			Egress:  digitsFreeLabel(name),
		}
	}

	return DisplayInfo{
		Name:    unknownCallerName,
		Display: joinDisplay(unknownCallerName, tail),
		Egress:  unknownCallerName,
	}
}

// joinDisplay appends the masked last-4 tail to a label. An empty tail
// (empty / digit-free number) renders NO parenthesis artifact at all
// (gate §1 pins "Unknown caller", not "Unknown caller ()").
func joinDisplay(label, tail string) string {
	if tail == "" {
		return label
	}
	return label + " (" + displayMaskPrefix + tail + ")"
}

// lastFourDigits strips every non-digit and returns the last 4 digits of
// what remains. Fewer than 4 digits -> the whole digit tail is returned
// (gate §2 short-number rule); no digits at all -> "" (the no-parens
// rule). This is the ONLY digit form allowed to leave the resolver.
func lastFourDigits(number string) string {
	var b strings.Builder
	for _, r := range number {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if len(digits) > 4 {
		return digits[len(digits)-4:]
	}
	return digits
}

// digitsFreeLabel is the egress hardener: the master FROZEN rule says
// Egress NEVER carries digits, so a DisplayName that contains digits is
// scrubbed for every egress surface (operators still see the raw name in
// Display/Name; only egress is name-no-digits).
func digitsFreeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// sqliteDisplayLookup is the O5-vendored daemon-local adapter over the
// EXISTING contact read API — (*contact.SQLiteStore).GetByPhone — so the
// seam is wireable in production BEFORE the caller-authz tree lands.
// TODO(caller-authz merge): (*contact.SQLiteStore) will satisfy
// CallerDisplayLookup structurally via its own LookupByPhone; this shim
// then goes away (see the O5 note at the top of this file). It is not
// wired at construction yet by design: leaf 01's contract is
// VoiceWiring.displayLookup default-nil; the wiring-site assignment is
// leaf 02's surface swap.

type sqliteDisplayLookup struct {
	store *contact.SQLiteStore
}

var _ CallerDisplayLookup = (*sqliteDisplayLookup)(nil)

// LookupByPhone adapts GetByPhone to the pinned signature: the store's
// ErrUnknownContact sentinel becomes (nil, false, nil) — not-found is a
// render path, not an error — while every other failure surfaces as an
// error so resolveDisplay can WARN + fail soft.
func (a *sqliteDisplayLookup) LookupByPhone(ctx context.Context, e164 string) (*contact.Record, bool, error) {
	if a == nil || a.store == nil {
		return nil, false, nil
	}
	rec, err := a.store.GetByPhone(ctx, e164)
	switch {
	case errors.Is(err, contact.ErrUnknownContact):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return &rec, true, nil
}
