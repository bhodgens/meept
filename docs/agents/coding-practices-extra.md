# Agent Invariants: Coding Practices (Full Text)

Moved from the root `AGENTS.md` to keep the root under the size cap. The root
keeps the hard rules; this file holds the full examples. Update both in the
same commit. The typed-nil/mutex/error section stays in
`docs/agents/coding-practices.md`.

## Coding Practices

### e2e Testing Policy

**All new feature tests go in the hermetic e2e tier — no new unit test files
for new features.** Existing unit tests may be updated for bug fixes only.

- Suites: `e2e/suites/<name>/` with the `e2e` build tag; manifest +
  path→suite mapping in `e2e/manifest.json`. See
  `docs/workflows/e2e-testing.md` for tiers, manifest format, and the
  coverage rule.
- `make e2e-fast` (all suites), `make e2e-fast-area AREA=<name>` (one suite),
  `make e2e-affected` (suites affected by the working diff, via
  `scripts/e2e-affected.sh`). The fast tier is hermetic and runs in CI
  (ci.yml job `e2e-fast`, a required member of the `ci-success` aggregate
  gate - that is the one blocking merges; code-quality.yml runs the same
  tier as advisory coverage); `make e2e-chat` is the live-model tier,
  local-only.
- Scratch-bin hygiene: the harness builds meept-daemon/meept once per test
  process into `$TMPDIR/meept-e2e-bin*`. A detached watchdog reaper
  (`e2e/harness/daemon.go`) removes that dir seconds after the test
  process exits AND kills any meept-daemon/meept processes running from
  that dir — the safety net for abnormal test-process exits (panic abort,
  os.Exit, kill -9), where in-process `t.Cleanup` never runs and suites
  that spawn daemons via raw `exec.Command` would otherwise leak them (78
  orphaned scratch daemons observed on one host). A 24h-age sweep runs at
  first harness use, and `make e2e-clean-tmp` is the standalone/
  belt-and-braces sweep wired into all three `make e2e-*` Go targets. Do
  not reintroduce an ownerless
  `os.MkdirTemp("", "meept-e2e-bin...")` — an earlier version of the
  harness leaked one ~136 MiB dir per test process (219 GiB observed).
- Enforcement: pre-commit check [19/20] (`pre-commit-e2e`) runs the new-
  package census — blocks commits that add files under a NEW package
  directory without an e2e suite + manifest entry — while the affected-
  suite sweep moved to the push boundary (`pre-push-e2e`, FULL scope) and
  CI (`e2e-fast`) after the gates-speedup change; opt it back in per-commit
  with `MEEPT_E2E_PRECOMMIT_SWEEP=1`. Emergency-only bypass:
  `MEEPT_SKIP_E2E=1 git commit` (prints a loud warning). When you add a
  package, add its `path_map` entry + suite to the manifest in the same
  commit.

### Predictable ID Prevention

**Never use `time.Now().UnixNano()` or `math/rand` for ID generation, even in
fallback paths.**

When `crypto/rand` fails, use `pkg/id.Generate()` which has a documented
zero-suffix fallback. The zero suffix indicates catastrophic system failure
(entropy exhaustion) rather than providing pseudo-random IDs that appear secure
but are predictable.

**Custom analyzers:** Run `go run ./tools/analyzers/predid/... ./...` to detect
predictable ID patterns.

### Optimization Posture

**Prefer early optimization over defensible-but-suboptimal defaults.** Pick the
optimized approach when cost/benefit is reasonable.

- Share genuinely-sharable state (configs, registries, builders) across instances
- Prefer structural isolation over convention-based isolation
- Don't over-analyze micro-optimations (<100KB memory, <10ms latency)

### Wiring/Integration Requirement

**Implementations MUST include wiring — data structures without user-facing
interfaces are INCOMPLETE.**

Every feature must answer: **"How does a user actually use this?"**

**Complete feature checklist:**
- [ ] **Core logic** — data structures, interfaces, business logic
- [ ] **At least ONE interface** — CLI (`cmd/meept/`), TUI (`internal/tui/`),
      GUI (`ui/flutter_ui/`), or HTTP API (`internal/comm/http/`)
- [ ] **Agent wiring** — dispatcher routing, intent classification, tool
      exposure (if agents should use it)
- [ ] **Tests**

**Exception for prototypes:** Experimental features can ship with partial wiring
if the PR explicitly notes which interfaces are deferred and why.

**Red flags:**
- Files only in `internal/` with no changes to `cmd/`, `internal/tui/`, `ui/`,
  or `internal/comm/http/`

### Typed-nil guards, setters, mutex scope, error handling, clean fixes

Full rules with code examples live in `docs/agents/coding-practices.md`. Short form:
guard typed-nil interface assignments; every `Set*` needs a nil guard (enforced by
`internal/tools/builtin/setters_test.go`); never hold a mutex across I/O; no ignored
errors or bare `panic(err)` (pre-commit enforced); two-value map assertions; prefer
clean architectural fixes over workarounds — never ship a workaround as the final
solution without user approval.

### Surface Unacted Observations

**At the end of every turn, surface to the user any observations you made but
did not act on.** Silent discoveries are lost discoveries.

While working on one task, the agent often notices adjacent problems — a
potential bug in a sibling code path, a misconfiguration, a stale comment, an
inconsistent pattern, a suspicious value. If the agent decides not to address
these within the current task scope (out of focus, lacking certainty, or out of
respect for the task boundary), it MUST still tell the user about them before
completing the turn.

**Format:** Append a brief **Observations** section at the end of the response.
List each observation with:

- **Location** — `file:line`
- **What** — a one-line description
- **Why not acted on** — the reason it was left out of this turn

Example:

```
Observations:
- internal/agent/loop.go:412 — error from planner is swallowed (no log). Not
  acted on: out of scope for this PR; needs its own investigation.
- config/models.json5:55 — capability "code" has no provider mapped. Not acted
  on: unclear if intentional; flagging for the user to confirm.
```

Do not bury observations inside a wall of prose. If there are no observations,
omit the section entirely (do not write "No observations.").

### Prefer Clean Architectural Fixes

**Always prefer clean architectural fixes over hacky workarounds, even when the
clean fix requires more work.** Hacky workarounds accumulate technical debt and
create fragile systems that are hard to reason about.

Examples of hacky patterns to avoid:
- **Content comparison for deduplication** — comparing serialized content
  instead of using proper identity keys (`id`, `session_id`, hash of source).
- **Suppressing symptoms** — catching/swallowing an error to make a test pass
  without understanding why the error occurs.
- **Patching around a root cause** — adding a special case downstream instead
  of fixing the upstream producer of bad data.

**When fixing a bug:**

1. **Trace the root cause** through the full data flow — from the observed
   symptom back to its origin. Do not stop at the first place you *could* patch.
2. **Fix it at the source** — change the code that produces the incorrect
   behavior, not the code that merely reacts to it.
3. **If a workaround is temporarily necessary**, add a `TODO` comment with the
   ticket/reference and a concrete plan for the proper fix:

   ```go
   // TODO(subagent-1234): Temporary dedup by content string. Replace with
   // proper id-based dedup once Dispatcher emits stable task IDs.
   if seen[task.Payload] { continue }
   ```

**Never ship a workaround as the final solution without explicit user
approval.** If the clean fix is too large for the current change, say so, get
approval for the temporary measure, and record the follow-up work as an issue
or TODO.

