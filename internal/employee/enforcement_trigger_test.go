package employee

// Gate test: escalation-trigger wildcard semantics pin (Contract C5, tree
// docs/plans/20261113-escalation-resume-and-hardening, leaf 05).
//
// This is a semantics PIN, not a bug test — it PASSES TODAY by design:
// matchesTrigger (enforcement.go, EscalateOnAction branch) compares
// trig.Match == action with EXACT string equality and no wildcard
// expansion. A live secretary constitution carries
// {on: action, match: "*"}; that trigger is inert, and must STAY inert
// on purpose. The gate value: if a future change ever gives "*" wildcard
// meaning here, every allowlisted employee call would route to
// RequiresPlan — an approve→execute→re-escalate livelock. This pin makes
// that regression loud. The only '*' special-case in enforcement.go is the
// tools_forbidden deny wildcard (enforcement.go:667) — untouched here.

import "testing"

// TestMatchesTrigger_ActionStarIsExactMatch: {on: action, match: "*"} is an
// EXACT action-string compare, not a wildcard — it fires for NO normal
// action string.
func TestMatchesTrigger_ActionStarIsExactMatch(t *testing.T) {
	trig := EscalationTrigger{On: EscalateOnAction, Match: "*", Reason: "wildcard-shaped row must stay inert"}
	for _, action := range []string{"shell_execute", "file_delete", "web_fetch", "", "*"} {
		if matchesTrigger(trig, action, "", nil) {
			t.Errorf("trigger {on:action, match:\"*\"} FIRED for action %q — '*' gained wildcard meaning; exact-match semantics violated (livelock risk)", action)
		}
	}
}

// TestMatchesTrigger_ActionExactStillFires: Match == action still fires —
// the exact-match escalation path is intact and was not broken by any
// wildcard change.
func TestMatchesTrigger_ActionExactStillFires(t *testing.T) {
	trig := EscalationTrigger{On: EscalateOnAction, Match: "shell_execute", Reason: "deletes need signoff"}
	if !matchesTrigger(trig, "shell_execute", "", nil) {
		t.Error("trigger {on:action, match:\"shell_execute\"} did NOT fire for action \"shell_execute\" — exact-match escalation broken")
	}
	if matchesTrigger(trig, "file_delete", "", nil) {
		t.Error("trigger {on:action, match:\"shell_execute\"} fired for a different action — exact compare lost")
	}
}
