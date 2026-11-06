//go:build e2e

// F10 override expiry — the REAL body for the agent-groups-10 gate.
//
// The gate stub (cluster_identity_member_test.go, TestAgentGroupsF10OverrideExpiry)
// is an unconditional dated Skipf with no body: a skip can never assert
// anything, so this companion file carries the actual F10 scenario and
// the stub stays untouched (gate files are not modified by leaf work).
// When the review agent re-arms the stub it should delete THIS file's
// duplication note — or simply delete the Skipf line and let this body
// stand as the scenario.
//
// Coverage (manifest agent-groups-10, F10 of the master matrix):
//   - an expired group_overrides.json5 entry is SKIPPED at daemon load
//     with exactly ONE Warn naming the subject (C8), and
//   - the expired override has no resolution effect (the tool the policy
//     table doesn't grant stays ungranted), while
//   - the clock is INJECTED via MEEPT_GROUPS_NOW (no sleeps, no real
//     time in the assertion).
package agentgroups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestAgentGroupsF10OverrideExpiryExpiredSkippedWithWarn drives a live
// daemon with an overrides file carrying one expired + one live entry,
// the clock pinned between the two expiries:
//
//	expired:  agent:past-bot  shell      expires 2026-10-01
//	live:     agent:win-bot   git_push   expires 2027-06-01
//	pinned:   MEEPT_GROUPS_NOW=2026-11-01T00:00:00Z
//
// GREEN requires:
//  1. boot succeeds (expired entries are skipped, never fatal);
//  2. exactly ONE Warn in the daemon log naming agent:past-bot;
//  3. no Warn claiming agent:win-bot expired.
func TestAgentGroupsF10OverrideExpiryExpiredSkippedWithWarn(t *testing.T) {
	entries := `[{"subject":"agent:past-bot","allow":["shell"],"reason":"old window","expires":"2026-10-01T00:00:00Z"},` +
		`{"subject":"agent:win-bot","allow":["git_push"],"reason":"deploy window","expires":"2027-06-01T00:00:00Z"}]`

	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			return os.WriteFile(
				filepath.Join(st.MeeptHome, "group_overrides.json5"),
				[]byte(entries), 0o600)
		}),
		harness.WithExtraEnv(map[string]string{
			// The injected clock (leaf 07 reader seam): pinned AFTER the
			// first expiry, BEFORE the second. No sleep-based expiry.
			"MEEPT_GROUPS_NOW": "2026-11-01T00:00:00Z",
		}),
	)

	// 1. The daemon booted (harness.Start fatals on boot failure) with
	// the overrides file present.
	if _, err := os.Stat(filepath.Join(s.MeeptHome, "group_overrides.json5")); err != nil {
		t.Fatalf("F10: overrides file missing after boot: %v", err)
	}

	// 2. Exactly one Warn names the expired subject. Poll briefly: the
	// load happens during NewComponents, before the HTTP listener.
	deadline := time.Now().Add(30 * time.Second)
	var expiredWarns []string
	for time.Now().Before(deadline) {
		expiredWarns = expiredWarns[:0]
		for _, line := range strings.Split(daemonLog(s), "\n") {
			if strings.Contains(line, "expired group_overrides") ||
				strings.Contains(strings.ToLower(line), "expired override") {
				if strings.Contains(line, "agent:past-bot") {
					expiredWarns = append(expiredWarns, line)
				}
			}
		}
		if len(expiredWarns) >= 1 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(expiredWarns) == 0 {
		t.Fatalf("F10: no Warn naming the expired override agent:past-bot in the daemon log "+
			"(expiry-skip-with-Warn absent — reader not wired);\nlog tail:\n%s",
			tail(daemonLog(s), 1200))
	}
	if len(expiredWarns) > 1 {
		t.Fatalf("F10: want exactly ONE expired-overrides Warn, got %d:\n%s",
			len(expiredWarns), strings.Join(expiredWarns, "\n"))
	}

	// 3. The live entry must NOT have been warned about.
	if strings.Contains(daemonLog(s), "agent:win-bot") &&
		strings.Contains(strings.ToLower(daemonLog(s)), "expired") {
		for _, line := range strings.Split(daemonLog(s), "\n") {
			if strings.Contains(line, "agent:win-bot") && strings.Contains(strings.ToLower(line), "expired") {
				t.Fatalf("F10: live override wrongly reported expired: %s", line)
			}
		}
	}
}

// TestAgentGroupsF10OverrideExpiryCLIContract pins the operator surface
// half of F10: the CLI list/prune path sees the same expiry split (the
// CLI runs against the sandbox MEEPT_HOME with the same pinned clock
// semantics via its wall clock — the file content, not the clock, is
// under test here), and `groups override prune` removes exactly the
// expired entry atomically.
func TestAgentGroupsF10OverrideExpiryCLIContract(t *testing.T) {
	entries := `[{"subject":"agent:past-bot","allow":["shell"],"reason":"old window","expires":"2026-10-01T00:00:00Z"},` +
		`{"subject":"agent:win-bot","allow":["git_push"],"reason":"deploy window","expires":"2099-06-01T00:00:00Z"}]`

	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			return os.WriteFile(
				filepath.Join(st.MeeptHome, "group_overrides.json5"),
				[]byte(entries), 0o600)
		}),
	)

	out, _ := s.RunCLI(t, 30*time.Second, true, "groups", "override", "list")
	if !strings.Contains(out, "agent:win-bot") {
		t.Fatalf("F10 CLI: live override missing from list:\n%s", out)
	}
	if !strings.Contains(out, "1 expired") {
		t.Fatalf("F10 CLI: list does not report the expired count:\n%s", out)
	}
	if strings.Contains(out, "agent:past-bot") {
		t.Fatalf("F10 CLI: default list leaked an expired entry:\n%s", out)
	}

	out, _ = s.RunCLI(t, 30*time.Second, true, "groups", "override", "list", "--all")
	if !strings.Contains(out, "agent:past-bot") || !strings.Contains(out, "expired") {
		t.Fatalf("F10 CLI: --all must show the expired entry marked:\n%s", out)
	}

	out, _ = s.RunCLI(t, 30*time.Second, true, "groups", "override", "prune")
	if !strings.Contains(out, "pruned 1") {
		t.Fatalf("F10 CLI: prune removed the wrong count:\n%s", out)
	}

	out, _ = s.RunCLI(t, 30*time.Second, true, "groups", "override", "list")
	if strings.Contains(out, "agent:past-bot") || !strings.Contains(out, "agent:win-bot") {
		t.Fatalf("F10 CLI: post-prune state wrong:\n%s", out)
	}
}
