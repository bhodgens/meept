package daemon

// caller_display_test.go — test gate 00T for the contact-display tree
// (docs/plans/20261110-contact-display/00T-tests.md), §1 resolver,
// §2 no-digit rules, §5 relay header.
//
// O5 FALLBACK (pinned for leaf 01-resolver): contact.LookupByPhone does
// NOT exist in this tree yet (it ships with plan/caller-authz). Per the
// master O5 note, leaf 01 MUST vendor a daemon-local resolver with the
// SAME SIGNATURE behind this exact seam.
//
// PINNED PRODUCTION CONTRACT (internal/daemon/caller_display.go, leaf 01
// MUST reuse these exact names — the O5 vendored resolver keeps this
// shape when the authz tree merges):
//
//	// CallerDisplayLookup mirrors the authz tree's
//	// (*contact.SQLiteStore).LookupByPhone signature verbatim.
//	type CallerDisplayLookup interface {
//	    LookupByPhone(ctx context.Context, e164 string) (*contact.Record, bool, error)
//	}
//
//	// DisplayInfo is the boundary-resolved caller identity (master FROZEN).
//	type DisplayInfo struct {
//	    Name    string // contact DisplayName or "Unknown caller"
//	    Display string // operator-safe: "Jane Doe (•••5567)" / "Unknown caller (•••1234)"
//	    Egress  string // name only — NEVER digits
//	}
//
//	// VoiceWiring gains the unexported field:
//	//   displayLookup CallerDisplayLookup
//	// (nil-safe: a nil lookup takes the unknown path, never panics)
//	// and the method:
//	//   func (w *VoiceWiring) resolveDisplay(ctx context.Context, e164 string) DisplayInfo
//	// A lookup ERROR must fail-soft to the unknown path AND log at WARN
//	// on w.logger (O5: error -> DisplayInfo zero -> render unknown).
//
// RED discipline: the only undefined symbols referenced here are
// DisplayInfo / CallerDisplayLookup / VoiceWiring.displayLookup /
// (*VoiceWiring).resolveDisplay — all owned by leaf 01. §5 drives the
// EXISTING renderCallRelay: its current header embeds rec.Number raw, so
// the header no-digit assertion is the runtime RED gating leaf 02.

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/voice"
)

// --- pinned seam fakes ---------------------------------------------------

// fakeDisplayLookup implements the pinned CallerDisplayLookup signature.
var _ CallerDisplayLookup = (*fakeDisplayLookup)(nil)

type fakeDisplayLookup struct {
	rec     contact.Record
	found   bool
	err     error
	gotKeys []string
}

func (f *fakeDisplayLookup) LookupByPhone(_ context.Context, e164 string) (*contact.Record, bool, error) {
	f.gotKeys = append(f.gotKeys, e164)
	if f.err != nil {
		return nil, false, f.err
	}
	if !f.found {
		return nil, false, nil
	}
	r := f.rec
	return &r, true, nil
}

// warnCounter is a slog.Handler that records emitted levels; §1 asserts a
// WARN fires on the lookup-error fail-soft path.
type warnCounter struct {
	mu     sync.Mutex
	levels []slog.Level
}

func (h *warnCounter) Enabled(context.Context, slog.Level) bool { return true }
func (h *warnCounter) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.levels = append(h.levels, r.Level)
	return nil
}
func (h *warnCounter) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnCounter) WithGroup(string) slog.Handler      { return h }
func (h *warnCounter) count(atLeast slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.levels {
		if l >= atLeast {
			n++
		}
	}
	return n
}

// newDisplayWiring wires a VoiceWiring carrying only what resolveDisplay
// touches (FROZEN contract: the resolver must stay nil-safe on the rest).
func newDisplayWiring(t *testing.T, lookup CallerDisplayLookup) (*VoiceWiring, *warnCounter) {
	t.Helper()
	warns := &warnCounter{}
	w := &VoiceWiring{
		displayLookup: lookup,
		logger:        slog.New(warns),
	}
	return w, warns
}

func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- §1 resolver -----------------------------------------------------------

func TestResolveDisplayKnownContact(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDisplayLookup{
		found: true,
		rec:   contact.Record{DisplayName: "Jane Doe", Phone: "+15551235567"},
	}
	w, _ := newDisplayWiring(t, fake)

	got := w.resolveDisplay(ctx, "+15551235567")
	if got.Name != "Jane Doe" {
		t.Errorf("Name = %q, want %q", got.Name, "Jane Doe")
	}
	if got.Display != "Jane Doe (•••5567)" {
		t.Errorf("Display = %q, want %q", got.Display, "Jane Doe (•••5567)")
	}
	if got.Egress != "Jane Doe" {
		t.Errorf("Egress = %q, want %q", got.Egress, "Jane Doe")
	}
	// The lookup must be keyed by the caller-passed E.164.
	if len(fake.gotKeys) != 1 || fake.gotKeys[0] != "+15551235567" {
		t.Errorf("lookup keys = %v, want exactly [\"+15551235567\"]", fake.gotKeys)
	}
}

func TestResolveDisplayUnknownNumber(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDisplayLookup{found: false}
	w, _ := newDisplayWiring(t, fake)

	got := w.resolveDisplay(ctx, "+15551230001")
	if got.Name != "Unknown caller" {
		t.Errorf("Name = %q, want %q", got.Name, "Unknown caller")
	}
	if got.Display != "Unknown caller (•••0001)" {
		t.Errorf("Display = %q, want %q", got.Display, "Unknown caller (•••0001)")
	}
	if got.Egress != "Unknown caller" {
		t.Errorf("Egress = %q, want %q", got.Egress, "Unknown caller")
	}
}

func TestResolveDisplayLookupErrorFailSoft(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDisplayLookup{err: fmt.Errorf("boom: store down")}
	w, warns := newDisplayWiring(t, fake)

	got := w.resolveDisplay(ctx, "+15551230001")
	// Same unknown path as the not-found case (O5: error -> zero -> unknown render).
	if got.Name != "Unknown caller" || got.Egress != "Unknown caller" {
		t.Errorf("error path = %+v, want Unknown caller forms", got)
	}
	if got.Display != "Unknown caller (•••0001)" {
		t.Errorf("error path Display = %q, want %q", got.Display, "Unknown caller (•••0001)")
	}
	if warns.count(slog.LevelWarn) == 0 {
		t.Error("lookup error must log at WARN (fail-soft is silent-lossy otherwise)")
	}
}

func TestResolveDisplayEmptyNumberNoParenArtifact(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDisplayLookup{found: false}
	w, _ := newDisplayWiring(t, fake)

	got := w.resolveDisplay(ctx, "")
	if got.Name != "Unknown caller" {
		t.Errorf("empty Name = %q, want %q", got.Name, "Unknown caller")
	}
	if got.Display != "Unknown caller" {
		t.Errorf("empty Display = %q, want %q (no parenthesis artifact)", got.Display, "Unknown caller")
	}
	if strings.ContainsAny(got.Display, "()•") {
		t.Errorf("empty Display carries an artifact: %q", got.Display)
	}
	if got.Egress != "Unknown caller" {
		t.Errorf("empty Egress = %q, want %q", got.Egress, "Unknown caller")
	}
}

func TestResolveDisplayNilLookupIsNilSafe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w, _ := newDisplayWiring(t, nil)

	got := w.resolveDisplay(ctx, "+15551230001")
	if got.Name != "Unknown caller" || got.Egress != "Unknown caller" {
		t.Errorf("nil lookup = %+v, want Unknown caller forms (no panic, no leak)", got)
	}
}

// --- §2 no-digit rules -------------------------------------------------------

var egressNoDigit = regexp.MustCompile(`^[^0-9]*$`)

func TestResolveDisplayEgressNeverCarriesDigits(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		lookup CallerDisplayLookup
		number string
	}{
		{"known", &fakeDisplayLookup{found: true, rec: contact.Record{DisplayName: "Jane Doe", Phone: "+15551235567"}}, "+15551235567"},
		{"unknown", &fakeDisplayLookup{}, "+15551230001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newDisplayWiring(t, tc.lookup)
			got := w.resolveDisplay(ctx, tc.number)
			if !egressNoDigit.MatchString(got.Egress) {
				t.Errorf("Egress %q carries digits — egress is name-only (master FROZEN)", got.Egress)
			}
		})
	}
}

func TestResolveDisplayDisplayCarriesOnlyLast4(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		lookup     CallerDisplayLookup
		number     string
		wantLast4  string
		fullDigits string
	}{
		{"known", &fakeDisplayLookup{found: true, rec: contact.Record{DisplayName: "Jane Doe", Phone: "+15551235567"}},
			"+15551235567", "5567", "15551235567"},
		{"unknown", &fakeDisplayLookup{}, "+15551230001", "0001", "15551230001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newDisplayWiring(t, tc.lookup)
			got := w.resolveDisplay(ctx, tc.number)
			if !strings.Contains(got.Display, "\u2022\u2022\u2022"+tc.wantLast4) {
				t.Errorf("Display %q missing last-4 form \u2022\u2022\u2022%s", got.Display, tc.wantLast4)
			}
			// Full digit run absent...
			if strings.Contains(got.Display, tc.fullDigits) {
				t.Errorf("Display %q leaks the full number", got.Display)
			}
			// ...and NOTHING but the last-4 run survives in the field.
			if d := digitsOf(got.Display); d != tc.wantLast4 {
				t.Errorf("digits(Display) = %q, want exactly %q", d, tc.wantLast4)
			}
		})
	}
}

func TestResolveDisplayShortNumberKeepsWholeTail(t *testing.T) {
	ctx := context.Background()
	w, _ := newDisplayWiring(t, &fakeDisplayLookup{})
	got := w.resolveDisplay(ctx, "+123")
	// leaf 01: fewer than 4 digits -> whole tail is fine; must still show SOMETHING
	// identifying and the Unknown caller name.
	if !strings.HasPrefix(got.Display, "Unknown caller") || !strings.Contains(got.Display, "23") {
		t.Errorf("short-number Display = %q, want Unknown-caller form containing the digit tail", got.Display)
	}
}

// --- §5 relay header ---------------------------------------------------------

// headerNoLongDigitRun: the relay header line must be free of \d{7,} runs
// (master FROZEN: header uses the Egress/display form; goal/cost digits in
// summaries are allowed elsewhere, the assertion is SCOPED to the header).
var headerNoLongDigitRun = regexp.MustCompile(`\d{7,}`)

func TestRenderCallRelayHeaderFreeOfRawNumber(t *testing.T) {
	rec := voice.CallRecord{
		CallID:     "cc-relay-1",
		Number:     "+15551230001",
		EmployeeID: "emp-9",
		Direction:  "incoming",
	}
	lines := []voice.TranscriptLine{
		{Role: "user", Text: "my callback number is 5558675309"},
		{Role: "assistant", Text: "got it"},
	}

	got := renderCallRelay(rec, lines, "hangup")
	if got == "" {
		t.Fatalf("renderCallRelay rendered nothing")
	}
	// Scope the no-digit assertion to the HEADER only: renderCallRelay
	// concatenates the header and the first transcript line with no
	// newline between them (existing quirk pinned here — leaf 02 must not
	// be failed by spoken digits in the BODY), so cut at the first
	// transcript marker.
	header := got
	if before, _, ok := strings.Cut(got, "caller: "); ok {
		header = before
	} else if before, _, ok := strings.Cut(got, "reply: "); ok {
		header = before
	}
	if headerNoLongDigitRun.MatchString(header) {
		t.Errorf("relay header leaks a raw digit run (\\d{7,}): %q — header must use the Egress/last-4 form (leaf 02 RED)", header)
	}

	// Transcript body is UNTOUCHED: spoken digits inside lines stay verbatim.
	if !strings.Contains(got, "5558675309") {
		t.Errorf("relay body must keep transcript digits verbatim, got %q", got)
	}
}

func TestRenderCallRelayLocalLegHeaderStillDigitFree(t *testing.T) {
	// local: prefix leg detection must survive the header swap (existing
	// behaviour, pinned so leaf 02 does not regress the leg word).
	rec := voice.CallRecord{CallID: "cc-relay-2", Number: "local:desk", EmployeeID: "emp-9", Direction: "incoming"}
	lines := []voice.TranscriptLine{{Role: "user", Text: "hello"}}
	got := renderCallRelay(rec, lines, "hangup")
	if !strings.Contains(got, "local leg") {
		t.Errorf("local-leg relay lost the leg word: %q", got)
	}
}
