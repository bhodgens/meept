// overrides_test.go — leaf 07 reader tests: injected clock proves expiry
// (no time.Now anywhere in the decision path), malformed entries error
// with named defects (never silent skips), and add-only semantics are
// pinned BY TYPE SHAPE (reflection over the Override field set).
package groups

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// at builds a fixed RFC3339 UTC instant for the fake clock.
func at(y int, m time.Month, d int, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

// fixedClock returns a now func pinned to t (the injected clock — the
// reader has no clock of its own, so this is the ONLY time source).
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestOverridesExpiryFilterWithInjectedClock pins T1's expiry contract:
// an expired entry is SKIPPED (into Expired) at load, an active one
// survives, and moving the clock proves the boundary (active up to but
// not including expires; dead at expires).
func TestOverridesExpiryFilterWithInjectedClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "group_overrides.json5")
	entries := []Override{
		{
			Subject: "agent:deploy-bot",
			Allow:   []string{"git_push"},
			Reason:  "deploy window",
			Expires: at(2026, 11, 5, 17, 0),
		},
		{
			Subject: "agent:oncall",
			Allow:   []string{"shell"},
			Reason:  "incident response",
			Expires: at(2027, 1, 1, 0, 0),
		},
	}
	if err := SaveOverrides(path, entries); err != nil {
		t.Fatalf("save: %v", err)
	}

	var warns []string
	load := func(now time.Time) *OverrideSet {
		t.Helper()
		set, err := LoadOverrides(path, OverrideLoadOptions{
			Now:  fixedClock(now),
			Warn: func(msg string) { warns = append(warns, msg) },
		})
		if err != nil {
			t.Fatalf("load at %v: %v", now, err)
		}
		return set
	}

	// Before the window: both active, no warning.
	set := load(at(2026, 11, 1, 12, 0))
	if set.ActiveCount() != 2 || set.ExpiredCount() != 0 {
		t.Fatalf("pre-window: active=%d expired=%d, want 2/0", set.ActiveCount(), set.ExpiredCount())
	}
	if len(set.Warnings) != 0 || len(warns) != 0 {
		t.Fatalf("pre-window emitted warnings: %v / %v", set.Warnings, warns)
	}

	// One Warn per load for the batch of expired entries (exactly one —
	// C8), naming the expired subject.
	set = load(at(2026, 11, 6, 9, 0))
	if set.ActiveCount() != 1 || set.ExpiredCount() != 1 {
		t.Fatalf("post-window: active=%d expired=%d, want 1/1", set.ActiveCount(), set.ExpiredCount())
	}
	if len(set.Warnings) != 1 || len(warns) != 1 {
		t.Fatalf("post-window: want exactly ONE Warn, got set=%v callback=%v", set.Warnings, warns)
	}
	if !strings.Contains(warns[0], "agent:deploy-bot") || !strings.Contains(warns[0], "1 expired") {
		t.Fatalf("Warn does not name the expired subject/batch: %q", warns[0])
	}
	if set.Active[0].Subject != "agent:oncall" {
		t.Fatalf("wrong entry skipped: active=%v", set.Active)
	}

	// Boundary: expires instant itself is already dead (now >= expires).
	set = load(at(2027, 1, 1, 0, 0))
	if set.ActiveCount() != 0 {
		t.Fatalf("at-expires: entry still active (now>=expires must be dead): %v", set.Active)
	}
	// One microsecond before: alive.
	set = load(at(2027, 1, 1, 0, 0).Add(-time.Microsecond))
	if set.ActiveCount() != 1 {
		t.Fatalf("just-before-expires: entry wrongly skipped: %v", set.Expired)
	}
}

// TestOverridesMalformedEntryNamedError pins T1's malformed contract:
// every defect class is a NAMED error wrapping ErrMalformedOverride —
// never a silent skip, and the whole load fails (fail closed).
func TestOverridesMalformedEntryNamedError(t *testing.T) {
	valid := Override{
		Subject: "agent:ok",
		Allow:   []string{"git_push"},
		Reason:  "window",
		Expires: at(2027, 1, 1, 0, 0),
	}
	cases := []struct {
		name    string
		mutate  func(*Override)
		wantSub string // the named offender in the error text
	}{
		{"empty subject", func(o *Override) { o.Subject = "" }, `subject ""`},
		{"bad subject form", func(o *Override) { o.Subject = "deploy-bot" }, "deploy-bot"},
		{"empty allow", func(o *Override) { o.Allow = nil }, "allow"},
		{"empty allow item", func(o *Override) { o.Allow = []string{"git_push", ""} }, "empty tool name"},
		{"empty reason", func(o *Override) { o.Reason = "" }, "reason"},
		{"missing expires", func(o *Override) { o.Expires = time.Time{} }, "expires"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := valid
			tc.mutate(&o)
			err := o.Validate()
			if err == nil {
				t.Fatalf("%s: entry validated (must be a named error)", tc.name)
			}
			if !errors.Is(err, ErrMalformedOverride) {
				t.Fatalf("%s: error does not wrap ErrMalformedOverride: %v", tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("%s: error does not name the offender %q: %v", tc.name, tc.wantSub, err)
			}
		})
	}

	// A malformed entry poisons the whole load (fail closed, not partial).
	path := filepath.Join(t.TempDir(), "group_overrides.json5")
	if err := SaveOverrides(path, []Override{valid}); err != nil {
		t.Fatalf("save valid: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Append a malformed JSON5 entry (missing expires) through hujson-
	// tolerant syntax to exercise the real decode path.
	raw = append(bytesTrimNewline(raw), []byte(`[{"subject":"agent:broken","allow":["shell"],"reason":"x"}]`)[0:0]...)
	broken := strings.Replace(string(raw), "]}", `,{"subject":"agent:broken","allow":["shell"],"reason":"x"}]}`, 1)
	if broken == string(raw) {
		// wrapper shape differs; write the array form directly
		broken = `[{"subject":"agent:ok","allow":["git_push"],"reason":"window","expires":"2027-01-01T00:00:00Z"},{"subject":"agent:broken","allow":["shell"],"reason":"x"}]`
	}
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatalf("write broken: %v", err)
	}
	_, err = LoadOverrides(path, OverrideLoadOptions{Now: fixedClock(at(2026, 11, 1, 0, 0))})
	if err == nil {
		t.Fatalf("load with a malformed entry succeeded (must fail closed)")
	}
	if !errors.Is(err, ErrMalformedOverride) {
		t.Fatalf("load error does not wrap ErrMalformedOverride: %v", err)
	}
	if !strings.Contains(err.Error(), "agent:broken") {
		t.Fatalf("load error does not name the malformed subject: %v", err)
	}
}

// bytesTrimNewline drops one trailing newline (helper for the mutation
// above).
func bytesTrimNewline(b []byte) []byte {
	return []byte(strings.TrimRight(string(b), "\n"))
}

// TestOverridesMalformedFileNamedError pins the FILE-level defect class:
// unparsable JSON5 is a named error, and a missing file is an EMPTY set
// (first use out of the box) — never an error.
func TestOverridesMalformedFileNamedError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json5")
	set, err := LoadOverrides(missing, OverrideLoadOptions{Now: fixedClock(at(2026, 11, 1, 0, 0))})
	if err != nil {
		t.Fatalf("missing file must be an empty set, got error: %v", err)
	}
	if set.ActiveCount() != 0 || set.ExpiredCount() != 0 {
		t.Fatalf("missing file: set not empty: %+v", set)
	}

	bad := filepath.Join(t.TempDir(), "broken.json5")
	if err := os.WriteFile(bad, []byte("{not json at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = LoadOverrides(bad, OverrideLoadOptions{Now: fixedClock(at(2026, 11, 1, 0, 0))})
	if !errors.Is(err, ErrMalformedOverride) {
		t.Fatalf("unparsable file: want ErrMalformedOverride, got %v", err)
	}
}

// TestOverridesRequiresInjectedClock pins the clock rule: a nil Now is a
// named error — the reader NEVER falls back to wall-clock.
func TestOverridesRequiresInjectedClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "group_overrides.json5")
	_, err := LoadOverrides(path, OverrideLoadOptions{})
	if err == nil {
		t.Fatalf("nil Now accepted (reader must require the injected clock)")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "now func is required") {
		t.Fatalf("nil-Now error does not name the contract: %v", err)
	}
}

// TestOverridesAddOnlyShape pins the STRUCTURAL add-only rule: the
// Override type has no deny field and no ceiling fields — an entry
// cannot express a denial or a memory/network/model widening, so the
// C8 "add only" guarantee needs no runtime enforcement.
func TestOverridesAddOnlyShape(t *testing.T) {
	typ := reflect.TypeOf(Override{})
	allowed := map[string]bool{"Subject": true, "Allow": true, "Reason": true, "Expires": true}
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !allowed[name] {
			got = append(got, name)
		}
	}
	if len(got) > 0 {
		t.Fatalf("Override grew fields outside the C8 shape: %v — deny/ceiling fields are FORBIDDEN here (add-only by type shape)", got)
	}
	// The forbidden shapes, named explicitly for the reviewer.
	for _, forbidden := range []string{"Deny", "ToolsDenied", "MemoryMaxScope", "NetworkClass", "AllowedModels", "FSRoots"} {
		if _, ok := typ.FieldByName(forbidden); ok {
			t.Fatalf("Override carries forbidden field %q (deny/ceiling widening)", forbidden)
		}
	}
}

// TestOverrideSetAllowsFor pins the query-clock behavior of AllowsFor:
// subject-exact, query-clock filtered, sorted+deduped, nil-safe on a nil
// set.
func TestOverrideSetAllowsFor(t *testing.T) {
	set := &OverrideSet{
		Active: []Override{
			{Subject: "agent:deploy-bot", Allow: []string{"git_push", "shell"}, Reason: "w", Expires: at(2027, 1, 1, 0, 0)},
			{Subject: "agent:other", Allow: []string{"web_fetch"}, Reason: "w", Expires: at(2027, 1, 1, 0, 0)},
			{Subject: "agent:deploy-bot", Allow: []string{"git_push", "docker_build"}, Reason: "w2", Expires: at(2027, 2, 1, 0, 0)},
		},
	}
	got := set.AllowsFor("agent:deploy-bot", at(2026, 11, 1, 0, 0))
	want := []string{"docker_build", "git_push", "shell"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("AllowsFor = %v, want sorted+deduped %v", got, want)
	}
	// After the first grant expires the second still applies.
	got = set.AllowsFor("agent:deploy-bot", at(2027, 1, 15, 0, 0))
	if strings.Join(got, ",") != "docker_build,git_push" {
		t.Fatalf("query-clock expiry wrong: %v", got)
	}
	// Subject-exact: another agent gets nothing of these.
	if got := set.AllowsFor("agent:unknown", at(2026, 11, 1, 0, 0)); got != nil {
		t.Fatalf("unknown subject returned %v, want nil", got)
	}
	// Nil set is safe.
	var nilSet *OverrideSet
	if got := nilSet.AllowsFor("agent:deploy-bot", at(2026, 11, 1, 0, 0)); got != nil {
		t.Fatalf("nil set returned %v, want nil", got)
	}
}

// TestOverridesRoundTripAndPrune pins T3's persistence contract: the
// wrapper form parses back through the C8 array form, PruneOverrides
// removes exactly the expired entries atomically, and pruning with
// nothing expired leaves the file untouched.
func TestOverridesRoundTripAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "group_overrides.json5")
	entries := []Override{
		{Subject: "agent:old", Allow: []string{"shell"}, Reason: "past window", Expires: at(2026, 10, 1, 0, 0)},
		{Subject: "agent:live", Allow: []string{"git_push"}, Reason: "current window", Expires: at(2027, 6, 1, 0, 0)},
	}
	if err := SaveOverrides(path, entries); err != nil {
		t.Fatalf("save: %v", err)
	}
	// JSON5-tolerant read back (the file carries comments-free indented
	// JSON but must parse through the same reader as hand-written C8).
	set, err := LoadOverrides(path, OverrideLoadOptions{Now: fixedClock(at(2026, 11, 1, 0, 0))})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if set.ActiveCount() != 1 || set.ExpiredCount() != 1 {
		t.Fatalf("round trip: active=%d expired=%d, want 1/1", set.ActiveCount(), set.ExpiredCount())
	}

	removed, err := PruneOverrides(path, fixedClock(at(2026, 11, 1, 0, 0)))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("prune removed %d, want 1", removed)
	}
	set, err = LoadOverrides(path, OverrideLoadOptions{Now: fixedClock(at(2026, 11, 1, 0, 0))})
	if err != nil {
		t.Fatalf("post-prune reload: %v", err)
	}
	if set.ActiveCount() != 1 || set.Active[0].Subject != "agent:live" {
		t.Fatalf("post-prune contents wrong: %+v", set)
	}

	// No-op prune: nothing expired => file untouched, zero removed.
	removed, err = PruneOverrides(path, fixedClock(at(2026, 11, 1, 0, 0)))
	if err != nil || removed != 0 {
		t.Fatalf("no-op prune: removed=%d err=%v", removed, err)
	}
}

// TestOverrideSeamDenyBeatsOverrideAndSpecCeiling pins the ExtraAllowsFor
// wire contract at the resolver: the per-subject seam rescues a spec-
// granted tool the policy table would otherwise deny... it must NOT —
// deny beats override (the committed resolve_test semantics); an override
// can only surface a tool the SPEC granted but no policy allow covers,
// and never widens the spec.
func TestOverrideSeamDenyBeatsOverrideAndSpecCeiling(t *testing.T) {
	policies := []Policy{
		{Name: "coders", ToolsAllowed: []string{"file_read"}, ToolsDenied: []string{"shell"}},
	}
	bindings := []Binding{{Subject: "group:coders", Policy: "coders", Scope: "daemon"}}
	overrides := &OverrideSet{
		Active: []Override{
			{Subject: "agent:deploy-bot", Allow: []string{"git_push", "shell"}, Reason: "w", Expires: at(2027, 1, 1, 0, 0)},
		},
	}
	r := NewResolver(policies, bindings, false, &ResolverOptions{
		NormalizeToolName: func(s string) string { return s },
		ExtraAllowsFor:    func(subject string) []string { return overrides.AllowsFor(subject, at(2026, 11, 1, 0, 0)) },
	})

	// git_push is IN the spec but uncovered by the policy allow: the
	// override surfaces it (add-only rescue of a spec tool).
	res := r.ResolveForAgent("deploy-bot", []string{"coders"}, []string{"file_read", "git_push", "shell"})
	joined := strings.Join(res.AllowedTools, ",")
	if joined != "file_read,git_push" {
		t.Fatalf("override seam: allowed=%q, want file_read,git_push (git_push rescued; denied shell must stay denied)", joined)
	}
	if got := strings.Join(res.DeniedTools, ","); got != "shell" {
		t.Fatalf("deny-beats-override violated: denied=%q, want shell", got)
	}

	// Spec ceiling: a tool NOT in the spec never appears via override.
	res = r.ResolveForAgent("deploy-bot", []string{"coders"}, []string{"file_read"})
	if strings.Contains(strings.Join(res.AllowedTools, ","), "git_push") {
		t.Fatalf("override widened the spec: %v", res.AllowedTools)
	}

	// Expired at the query clock: no effect at all.
	expired := &OverrideSet{
		Active: []Override{
			{Subject: "agent:deploy-bot", Allow: []string{"git_push"}, Reason: "w", Expires: at(2026, 10, 1, 0, 0)},
		},
	}
	r2 := NewResolver(policies, bindings, false, &ResolverOptions{
		NormalizeToolName: func(s string) string { return s },
		ExtraAllowsFor:    func(subject string) []string { return expired.AllowsFor(subject, at(2026, 11, 1, 0, 0)) },
	})
	res = r2.ResolveForAgent("deploy-bot", []string{"coders"}, []string{"file_read", "git_push"})
	if strings.Join(res.AllowedTools, ",") != "file_read" {
		t.Fatalf("expired override had effect: allowed=%v", res.AllowedTools)
	}
}

// TestOverrideSeamUnmanagedIgnored pins the D7 boundary: an unmanaged
// subject (no bindings, default_managed=false) never consults the
// override seam — overrides apply to managed subjects only.
func TestOverrideSeamUnmanagedIgnored(t *testing.T) {
	consulted := false
	r := NewResolver(nil, nil, false, &ResolverOptions{
		ExtraAllowsFor: func(string) []string {
			consulted = true
			return []string{"shell"}
		},
	})
	res := r.ResolveForAgent("free-agent", nil, []string{"file_read"})
	if res.Managed {
		t.Fatalf("unmanaged subject resolved managed: %+v", res)
	}
	if consulted {
		t.Fatalf("override seam consulted for an unmanaged subject (must be managed-only)")
	}
}
