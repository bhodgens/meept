// groups_overrides_wiring.go — daemon wiring of the leaf-07 time-boxed
// overrides reader (agent-groups plan tree, contract C8).
//
// Clock discipline: EnvGroupsNow is THE injected clock — MEEPT_GROUPS_NOW
// (RFC3339) pins it for tests and the F10 e2e gate; unset falls through
// to wall-clock UTC. The reader's decision path has no time.Now of its
// own; every caller passes this func.
//
// Failure posture: a malformed overrides FILE or entry fails daemon boot
// with a named error (fail closed — an unreadable security artifact must
// never degrade silently); expired entries are skipped with one Warn at
// load (the reader emits, the boot path logs).
package daemon

import (
	"fmt"
	"os"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// EnvGroupsNow is the injected decision clock for the overrides reader
// and the resolver's override seam. MEEPT_GROUPS_NOW (RFC3339, e.g.
// 2026-11-05T17:00:00Z) pins it — the e2e harness injects the env var into
// the daemon process so F10 can prove expiry without sleeping. Unset =>
// wall-clock UTC. A malformed pinned value FAILS (never silently ignored:
// a test pinned to a typo'd clock must not run on real time).
func EnvGroupsNow() time.Time {
	if pinned := os.Getenv("MEEPT_GROUPS_NOW"); pinned != "" {
		t, err := time.Parse(time.RFC3339, pinned)
		if err != nil {
			// The daemon cannot start with an unreadable clock — this
			// only fires when the env var is set, so production is
			// unaffected; tests fail loudly instead of lying.
			panic(fmt.Sprintf("MEEPT_GROUPS_NOW is not RFC3339: %v", err))
		}
		return t
	}
	return time.Now().UTC()
}

// groupsOverridesPath resolves $MEEPT_HOME/group_overrides.json5.
// config.MeeptPath is THE resolution point (AGENTS.md).
func groupsOverridesPath() string {
	return config.MeeptPath(groups.OverridesFileName)
}
