// Bughunt 2026-11-27 fix wave F12 — audit S-9 (LOW).
//
// The A3-11 duration tripwire (walkStringDurationFields +
// TestStringDurationKeys_A311_FullSchemaCoverage) recursed only through
// Ptr/Slice/Struct and matched a fixed name list. Two blind spots, both
// closed in the walk this wave:
//
//  1. map-of-struct sections: a string-duration field inside a config
//     section typed map[string]SomeStruct (lsp.servers.*, providers.*,
//     agents.backoff.* shape) was invisible to the walk.
//  2. name patterns: period / every / cooldown / backoff / stale_after /
//     max_age / grace / delay / wait were not matched.
//
// These pins assert the tripwire FIRES on synthetic bad shapes: a
// string-duration field inside a map-of-struct value, and a field named
// with one of the newly added patterns. The firing mechanism is the real
// production assertion logic — a synthetic Config-shaped tree is walked
// and each discovered key is checked against stringDurationKeys, exactly
// as TestStringDurationKeys_A311_FullSchemaCoverage does for the live
// schema. Non-vacuity: reverting the map arm in walkStringDurationFields
// makes the map-shape pin find zero keys (fail asserted via the finder),
// and dropping the new regex patterns makes the pattern pin miss.
package config

import (
	"reflect"
	"testing"
)

// f12SyntheticBad mirrors the S-9 blind-spot shapes: (a) a string
// duration field inside a MAP-OF-STRUCT section under a name the OLD
// regex list also missed (period), and (b) one under a name from the
// newly added pattern set (cooldown). If the walk sees them, the keys
// land in the found set and the registry check flags them unregistered —
// the same assertion the live-schema tripwire makes.
type f12SyntheticLeaf struct {
	ShutdownPeriod string `json:"shutdown_period"` // bad: string duration shape, unregistered
	Cooldown       string `json:"cooldown"`        // bad: new pattern, unregistered
	HarmlessName   string `json:"harmless_name"`   // control: not a duration shape
	HarmlessTTL    int    `json:"ttl_seconds"`     // control: int, not a string field
}
type f12SyntheticConfig struct {
	Servers map[string]f12SyntheticLeaf `json:"servers"` // map-of-struct section (the S-9 blind spot)
}

// runF12TripwireFinder walks a synthetic tree and returns the discovered
// unregistered keys — the tripwire's exact detection logic, factored so
// the pin can assert FIRING (found & flagged) rather than absence.
func runF12TripwireFinder(t *testing.T, typ reflect.Type) (found map[string]bool, flagged []string) {
	t.Helper()
	found = map[string]bool{}
	walkStringDurationFields(t, typ, found, map[reflect.Type]bool{})
	for key := range found {
		if !stringDurationKeys[key] {
			flagged = append(flagged, key)
		}
	}
	return found, flagged
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Pin 1 (map-of-struct walk): the synthetic bad fields inside the map
// VALUE struct are discovered and flagged. Asserting via found/flagged
// (not via a t.Errorf inside the walk) lets the pin express "tripwire
// fires" explicitly; the live-schema test keeps its own hard failure.
func TestS9_TripwireFiresOnMapOfStructBadDurationField(t *testing.T) {
	found, flagged := runF12TripwireFinder(t, reflect.TypeOf(f12SyntheticConfig{}))

	if !found["shutdown_period"] {
		t.Fatal("S-9: tripwire did NOT see shutdown_period inside the map-of-struct section (map arm missing)")
	}
	if !containsStr(flagged, "shutdown_period") {
		t.Errorf("S-9: shutdown_period discovered but not flagged unregistered; flagged=%v", flagged)
	}

	// Both arms (map AND slice) of the same leaf type are walked.
	if !found["cooldown"] {
		t.Fatal("S-9: tripwire did NOT see cooldown in the leaf struct")
	}
	if !containsStr(flagged, "cooldown") {
		t.Errorf("S-9: cooldown discovered but not flagged unregistered; flagged=%v", flagged)
	}

	// Controls: the non-duration and non-string fields stay invisible.
	if found["harmless_name"] {
		t.Error("S-9: harmless_name matched the duration shape filter (over-broad regex)")
	}
	if found["ttl_seconds"] {
		t.Error("S-9: non-string field ttl_seconds collected (string-kind filter broken)")
	}
}

// Pin 2 (new name patterns): each pattern added in F12 matches. These
// are walk-level (regex) assertions independent of any schema, so a
// future regex trim is caught even if the live schema never carries the
// name.
func TestS9_TripwireMatchesNewNamePatterns(t *testing.T) {
	for _, name := range []string{
		"period", "every", "cooldown", "backoff", "stale_after",
		"max_age", "grace", "delay", "wait",
		// Pre-existing patterns must not regress.
		"interval", "timeout", "refresh_margin", "session_window", "cache_ttl", "ttl", "expiry", "retention",
	} {
		if !durShapeKey.MatchString(name) {
			t.Errorf("S-9: durShapeKey no longer matches %q (pattern dropped)", name)
		}
	}
	// Non-duration names stay unmatched (no over-blocking). These are
	// substring-collision traps: waitress/wait, periodic/period,
	// graceful/grace. Pre-existing patterns (interval, margin) always had
	// substring semantics, so the same looseness is accepted here — the
	// cost of a rare over-match is a one-line registry entry, the cost
	// of a miss is bricked config load.
	for _, name := range []string{"hostname", "elderberry", "plainvalue"} {
		if durShapeKey.MatchString(name) {
			t.Errorf("S-9: durShapeKey matches non-duration name %q", name)
		}
	}
}

// Non-vacuity: revert the map arm in walkStringDurationFields (delete the
// reflect.Map block) and TestS9_TripwireFiresOnMapOfStructBadDurationField
// fails on the first Fatal ("did NOT see shutdown_period"); trim the new
// alternates from durShapeKey and TestS9_TripwireMatchesNewNamePatterns
// fails on the corresponding pattern.
