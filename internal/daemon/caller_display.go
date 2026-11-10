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
// O5 MERGE NOTE (resolved 2026-11-10): contact.LookupByPhone shipped with
// plan/caller-authz and is merged; the daemon resolves display identity
// through the store STRUCTURALLY (no vendored shim anymore). Callers
// normalize the E.164 via voice.NormalizeE164 before lookup, matching
// daemonCallerResolver.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/voice"
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
	if w == nil {
		return resolveDisplayFor(ctx, nil, nil, e164)
	}
	return resolveDisplayFor(ctx, w.displayLookup, w.logger, e164)
}

// resolveDisplayFor is the shared boundary resolver behind
// (*VoiceWiring).resolveDisplay, exposed so the VoiceEmployeeWiring relay
// surface (leaf 02) resolves through the SAME logic without owning a
// VoiceWiring back-reference. Nil-safe: a nil lookup/logger/ctx degrades
// to the unknown path.
func resolveDisplayFor(ctx context.Context, lookup CallerDisplayLookup, logger *slog.Logger, e164 string) DisplayInfo {
	// The identifying tail comes from the number itself, so the unknown
	// path can render it even when the store is down (master: "store
	// error -> render Unknown caller (+last4 from the number itself)").
	tail := lastFourDigits(e164)

	var (
		rec   *contact.Record
		found bool
	)
	if ctx == nil {
		ctx = context.Background()
	}
	if lookup != nil {
		lctx, cancel := context.WithTimeout(ctx, displayResolveTimeout)
		defer cancel()
		var err error
		rec, found, err = lookup.LookupByPhone(lctx, e164)
		if err != nil {
			// O5 fail-soft: the store error degrades to the unknown
			// render; WARN keeps the degradation observable.
			if logger != nil {
				logger.Warn("caller display: contact lookup failed, rendering unknown caller",
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

// (2026-11-10 O5 swap: the vendored sqliteDisplayLookup shim is deleted —
// *contact.SQLiteStore satisfies CallerDisplayLookup structurally via its
// exact-match LookupByPhone, wired directly by newDisplayLookupSeam.)

// newDisplayLookupSeam adapts an (optional) contacts store onto the
// resolver seam without the typed-nil trap: a nil *SQLiteStore returns an
// INTERFACE-nil seam, so every surface takes the unknown-caller path.
// Since the authz merge the store satisfies CallerDisplayLookup directly;
// lookups are exact-match on the stored E.164, so call sites normalize the
// number via voice.NormalizeE164 before resolving (matching
// daemonCallerResolver).
func newDisplayLookupSeam(store *contact.SQLiteStore) CallerDisplayLookup {
	if store == nil {
		return nil
	}
	return normalizingDisplayLookup{store: store}
}

// normalizingDisplayLookup adapts the store's EXACT-match LookupByPhone
// for display callers, which may hand raw-form numbers (the registry
// target is the carrier's from-string). The raw string is tried first;
// on a miss the voice.NormalizeE164 form (spaces/dashes/parens stripped,
// country code folded) is retried when it differs, so both storage
// conventions resolve.
type normalizingDisplayLookup struct{ store *contact.SQLiteStore }

var _ CallerDisplayLookup = normalizingDisplayLookup{}

func (n normalizingDisplayLookup) LookupByPhone(ctx context.Context, e164 string) (*contact.Record, bool, error) {
	rec, found, err := n.store.LookupByPhone(ctx, e164)
	if err != nil || found {
		return rec, found, err
	}
	if norm := voice.NormalizeE164(e164); norm != "" && norm != e164 {
		return n.store.LookupByPhone(ctx, norm)
	}
	return nil, false, nil
}
