# e2e Testing

This repo has two e2e tiers plus a policy that decides where new feature
tests go. Enforcement: the new-package census lives in the pre-commit hook
(check [19/20]); the affected-suite sweep runs at the push boundary
(`pre-push-e2e`) and in CI — see "Where the sweep runs" below.

## Tiers

| Tier | Command | What it is |
|------|---------|------------|
| **Fast (hermetic)** | `make e2e-fast` | Go tests under `e2e/suites/` with the `e2e` build tag. Fresh binaries, sandboxed `HOME`/`MEEPT_HOME`, fake LLM. Never touches `~/.meept` or a live daemon. Seconds per suite; the full tier is ~13 min wall (`-p 2`, 63 suites, measured 2026-10-25). Runs in CI (ci.yml, job `e2e-fast`, a member of the `ci-success` aggregate gate; code-quality.yml runs the same tier as `e2e-fast` too) and at the push boundary via `pre-push-e2e` (the affected-suite sweep). Since the gates-speedup change the sweep is NO LONGER part of pre-commit — see "Where the sweep runs" below. |
| **Fast, Dart tier** | `make e2e-affected-area AREA=gui-flows` | The `gui-flows` suite: Flutter widget/provider tests under `ui/flutter_ui/test/e2e`, driven by `flutter test test/e2e` (NOT `go test`). Runs in CI (code-quality.yml, job `e2e-gui`) and via the affected-suite gate when a `ui/**` path changes. |
| **Live-model** | `make e2e-chat` | `scripts/e2e-naive-user-chat.sh` drives a real daemon with a real model through a naive-user chat comparison. Slow, costs tokens, needs a configured local daemon. Local-only by design — never in CI. Its scenario also ships as the hermetic twin `e2e/suites/naive-user-chat/` (manifest-registered; CI + pre-commit coverage for the A0-A6 contract). |

Single suite: `make e2e-fast-area AREA=smoke` (Go) or
`make e2e-affected-area AREA=gui-flows` (any tier, Go or Dart).

## Writing a suite

Suites live in `e2e/suites/<name>/` (one package per suite dir) and carry the
`//go:build e2e` tag so normal `go test ./...` never compiles them. Pattern:

```go
//go:build e2e

package smoke

import "testing"
```

Conventions:

- Each suite dir is self-contained: it builds the binaries it needs, points
  `MEEPT_HOME` at a throwaway dir, and tears everything down. See
  `e2e/suites/smoke/` for the reference implementation.
- One suite = one area of behavior (rpc, chat-submit, tools-filesystem, ...).
  Keep the name stable — the manifest, the hook, and CI all reference it.
- Tests must be hermetic: no live model calls, no user config, no fixed
  ports that collide under `-p 2`.
- Binaries: use `harness.CLIPath(t)` / `harness.DaemonPath(t)` — never call
  `os.MkdirTemp` for binaries yourself. The harness builds once per test
  process into `$TMPDIR/meept-e2e-bin*` and a detached watchdog removes
  that dir seconds after the process exits AND kills every meept-daemon /
  meept process whose executable lives in that dir (covering panics,
  os.Exit, and kill -9, which in-process `t.Cleanup` cannot; scoped by
  exec-path prefix so the user's live daemon and concurrent e2e runs are
  never touched). This daemon-kill layer exists because suites spawn
  daemons with raw `exec.Command` under `t.Cleanup` only — an abnormal
  test-process exit used to orphan them (78 leaked daemons observed). Two
  fallback layers clear dirs orphaned by a killed process group: a 24h-age
  sweep at first harness use and `make e2e-clean-tmp` (wired into
  `e2e-fast`, `e2e-fast-area`, and `e2e-affected`).

## Manifest format (`e2e/manifest.json`)

Two structures matter to the tooling:

```json
{
  "path_map": {
    "internal/rpc/": ["rpc-ping", "chat-submit", "session-binding"],
    "internal/comm/": ["ws-classification", "ws-filter", "http-auth", "http-stream"],
    "ui/flutter_ui/": ["gui-flows"]
  },
  "suites": [
    {"name": "smoke", "dir": "e2e/suites/smoke", "status": "implemented"},
    {"name": "rpc-ping", "dir": "e2e/suites/rpc-ping", "status": "todo"},
    {"name": "gui-flows", "dir": "ui/flutter_ui/test/e2e", "status": "implemented",
     "runner": "dart", "command": ["flutter", "test", "test/e2e"],
     "workdir": "ui/flutter_ui"}
  ],
  "scenarios": [
    {"id": "rpc-ping-01", "suite": "rpc-ping", "diff": "S",
     "title": "ping round-trip ...", "paths": ["internal/rpc/server.go"]}
  ]
}
```

- `path_map`: repo path prefix → suite names. A changed file under the prefix
  marks those suites affected. A package directory with NO entry here counts
  as NEW for the coverage rule (below).
- `suites[].dir`: where the suite's tests live. `status: todo` suites have no
  dir yet; the affected script reports them as pending and skips them.
- `suites[].runner`: `go` (default) or `dart`. A non-Go runner MUST also carry
  `command` (argv) and `workdir`; the affected script dispatches on it instead
  of passing the dir to `go test`. `gui-flows` is the only Dart tier today.
- `scenarios`: the human/agent-readable inventory of what each suite covers
  (id, owning suite, size estimate, one-line title, representative paths).

When you add a package or suite, update this manifest in the same commit.

**Manifest completeness is pinned, not policed.** `e2e/suites/manifest/`
(`manifest-01`) asserts that every declared suite is reachable from some
`path_map` key, that every `path_map` key names a declared suite, that every
`.dart` file under `ui/flutter_ui/lib/` selects a suite, and that every
scenario's `paths` match a key. A suite no key selects can never run, and it
fails silently — which is exactly how `gui-flows` sat unreachable for the
lifetime of its five scenarios.

## Affected script (`scripts/e2e-affected.sh`)

Maps changed paths through the manifest and runs only what's affected:

```bash
scripts/e2e-affected.sh --list                 # print affected suite names only
scripts/e2e-affected.sh internal/rpc/foo.go    # paths as args
git diff --name-only HEAD | scripts/e2e-affected.sh   # paths on stdin
make e2e-affected                              # same, --from-diff
```

Behavior:

- Affected Go suite dirs that exist → `go test -tags e2e -count=1 -p 2` on them.
- **File-level narrowing on hub prefixes.** A catch-all package key like
  `internal/daemon/` used to select nine suites for ANY file in the package,
  so a one-line `voice_wiring.go` change ran the memory/agent/queue suites
  that never touch it. Hub prefixes now carry ONLY whole-subject suites
  (`daemon-lifecycle`: boot/pid/restart hygiene, breakable by any daemon
  change); file-specific selection comes from exact-file `path_map` keys
  DERIVED from scenario evidence (each scenario's `paths[]` lists the source
  files its suite exercises). Re-derive after adding scenarios that cite new
  hub files: `python3 scripts/e2e-pathmap-narrow.py --write` (pinned by
  `TestNarrowCatchallKeysAreScenarioDerived` in the manifest suite, which
  fails when the derivation and the evidence drift apart).
- Affected suites with `runner: dart` → their `command` run from their
  `workdir` (skipped with a printed note when the Flutter toolchain is
  absent). A failing Dart suite fails the gate exactly like a Go one.
- Affected suites still `todo` → reported and skipped.
- No path_map hit but Go files changed → runs the smoke suite as fallback so
  the gate never silently passes.
- Dart files changed but nothing mapped → **exit 1** (there is no Go suite to
  fall back to, and passing silently would be the same decorative gate).
- Both tiers can be affected at once; each runs, and the worst exit code wins.
- No relevant paths (docs only, etc.) → no-op, exit 0.

bash-3.2 compatible (macOS); python3 does the JSON parsing.

## Where the sweep runs (`pre-commit-e2e` census [19/20] → `pre-push-e2e` → CI)

Gate locations after the gates-speedup change (the sweep cost 15-25 min
per commit touching `internal/queue|agent|task`, so it moved off the
commit boundary):

**Pre-commit `[19/20]` (`pre-commit-e2e`) runs ONLY the census** on staged
Go changes:

1. **New-package census** (stays — cheap, blocks forgetting) — every
   package directory that gains a new non-test Go file in the staged
   additions must have a `path_map` entry in `e2e/manifest.json` or an
   `exempt` entry with a written reason (printed as residual risk), or
   the commit FAILS. Exempt: `_test.go`, docs, generated files. Staging
   e2e suite files no longer clears the check; an unparseable or empty
   manifest fails the commit closed.

The **affected-suite sweep** is no longer part of pre-commit. It runs at
two enforcement sites:

- `pre-push-e2e` (push boundary) with `MEEPT_E2E_SCOPE=full` FORCED, and
- CI's `make e2e-fast` (ci.yml job `e2e-fast`, required `ci-success`
  member).

Want the sweep back at commit time anyway? Opt in per invocation:

```bash
MEEPT_E2E_PRECOMMIT_SWEEP=1 git commit -m "..."          # full sweep
MEEPT_E2E_PRECOMMIT_SWEEP=1 MEEPT_E2E_SCOPE=direct git commit ...  # package-dir-only, cheap
```

Escape hatch for emergencies — prints a loud warning and proceeds
(disables the census too):

```bash
MEEPT_SKIP_E2E=1 git commit -m "..."
```

`--no-verify` also bypasses (bypasses all 20 checks — emergencies only).

### Sweep scope (`MEEPT_E2E_SCOPE`)

`scripts/e2e-affected.sh` selects suites from the changed paths through
the manifest `path_map`:

- `full` (default; what the push gate forces): a path matching a
  `path_map` PREFIX contributes that key's suites — so a change to
  `internal/queue/queue.go` fans out to everything any key covering
  `internal/queue/` (and ancestor keys covering the file path) maps.
  Feature Go changed with no hit at all → the smoke suite runs as the
  non-decorative fallback.
- `direct`: a path selects suites ONLY from the key equal to its own
  package dir (`internal/kanban/card.go` -> `kanban-board`; nothing
  else). No fan-out, no smoke fallback — nothing mapped selects
  nothing, loudly printed. This is the local-dev knob for running the
  sweep where the transitive fan-out would sweep half the manifest;
  `pre-push-e2e` pins `full` so it can never be thinned by accident.

### Flake retry

A failing suite gets ONE labeled re-run (boot flakes pass in isolation),
but since the gates-speedup change the retry is parallel across suites
(`MEEPT_E2E_RETRY_PARALLEL`, default 2) instead of a serial 2x-timeout
per failure, and a suite whose output shows a BUILD/setup failure is NOT
retried at all (`SKIPRETRY(build)`) — a compile error never flakes.

## The push-boundary sweep gate (`pre-push-e2e`)

`pre-commit-e2e` no longer runs the suite sweep at commit time (the census
does), so `pre-push-e2e` is the first local sweep run: on every `git push`
it diffs each pushed ref (`remote_sha..local_sha`), feeds the changed
paths to `scripts/e2e-affected.sh` with `MEEPT_E2E_SCOPE=full` FORCED (an
ambient `direct` can never thin the push gate), and runs exactly the
affected hermetic suites — same mapping, same suite runner. A red suite
blocks the push. It also still closes the old bypass hole (`--no-verify`,
`MEEPT_SKIP_E2E=1`, or a killed hook run at commit time). A new branch
with no tracking ref runs the smoke suite instead, so the gate never
degrades to a no-op.

```bash
MEEPT_E2E_SKIP=1 git push   # loud skip of this gate only (emergencies)
```

CI's `make e2e-fast` (ci.yml, job `e2e-fast`; also run by code-quality.yml's
`e2e-fast` job) is the third, remote-enforced line; the push gate is what
makes the local bypass non-shippable before CI even starts. The ci.yml job
is a member of that workflow's `ci-success` aggregate gate, so a red
hermetic suite fails the required status check - the code-quality.yml copy
is advisory from that gate's point of view.

## CI coverage details

The ci.yml `e2e-fast` job runs the FULL hermetic tier (`make e2e-fast` =
`go test -tags e2e -count=1 ./e2e/... -p 2`) on ubuntu-latest:

- No live models: the harness routes every alias at the in-process fake LLM
  (`e2e/harness/fakellm.go`); nothing dials an external endpoint.
- No host-state dependence: every suite builds scratch binaries into
  `$TMPDIR/meept-e2e-*`, points `MEEPT_HOME`/`HOME` inside that sandbox,
  and the harness cleans up after itself (watchdog reaper + 24h sweep +
  `e2e-clean-tmp`).
- Job budget: 30 minutes against a measured ~13 min local wall; the
  alsa headers step matches the other Go jobs (cgo oto dependency).
- The Dart tier (`gui-flows`) is NOT in this job - it needs the Flutter
  SDK and runs in code-quality.yml's `e2e-gui` job.

## NO-NEW-UNIT-TESTS policy

**All new feature tests go in the e2e tier. Existing unit tests may be
updated for bug fixes, but no new unit test files may be created for new
features.**

Why: unit tests at package scope kept passing while cross-boundary behavior
(chat turn lifecycle, session binding, WS classification, restart persistence)
regressed. The e2e tier exercises real binaries over real transports in a
sandboxed home, which is where these bugs actually lived. The per-package
unit suite still has value for regressions in already-tested logic; it is no
longer the default home for new coverage.

Practical rules:

- New feature/package → write tests in `e2e/suites/<your-suite>/` + add the
  manifest entry. The hook blocks the commit otherwise.
- Bug fix in existing code → updating the existing unit test file is fine
  (that's a fix, not a new unit test).
- Renaming/moving an existing unit test wholesale into a new `_test.go` to
  dodge the policy is a violation, not a workaround.
