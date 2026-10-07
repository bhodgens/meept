# e2e Testing

This repo has two e2e tiers plus a policy that decides where new feature
tests go. Enforcement lives in the pre-commit hook (check [18/18]).

## Tiers

| Tier | Command | What it is |
|------|---------|------------|
| **Fast (hermetic)** | `make e2e-fast` | Go tests under `e2e/suites/` with the `e2e` build tag. Fresh binaries, sandboxed `HOME`/`MEEPT_HOME`, fake LLM. Never touches `~/.meept` or a live daemon. Seconds per suite. Runs in CI (code-quality.yml, job `e2e-fast`) and in the pre-commit hook via the affected-suite gate. |
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
  that dir seconds after the process exits (covering panics and kill -9,
  which in-process `t.Cleanup` cannot). Two fallback layers clear dirs
  orphaned by a killed process group: a 24h-age sweep at first harness use
  and `make e2e-clean-tmp` (wired into `e2e-fast`, `e2e-fast-area`, and
  `e2e-affected`).

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

## The hook (`pre-commit-e2e`, check [18/18])

Runs on every commit with staged Go files:

1. **Affected-suite gate** — staged `internal/|pkg/|cmd/` Go paths are fed to
   `scripts/e2e-affected.sh`; any failing suite FAILS the commit.
2. **New-feature coverage rule** — if a staged change creates files under a
   NEW package directory (no `path_map` entry) AND no e2e suite files are
   staged, the commit FAILS with instructions to add e2e coverage and a
   manifest entry. Exempt: `_test.go`, docs, generated files.

Escape hatch for emergencies — prints a loud warning and proceeds:

```bash
MEEPT_SKIP_E2E=1 git commit -m "..."
```

`--no-verify` also bypasses (bypasses all 18 checks — emergencies only).

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
