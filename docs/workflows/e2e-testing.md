# e2e Testing

This repo has two e2e tiers plus a policy that decides where new feature
tests go. Enforcement lives in the pre-commit hook (check [18/18]).

## Tiers

| Tier | Command | What it is |
|------|---------|------------|
| **Fast (hermetic)** | `make e2e-fast` | Go tests under `e2e/suites/` with the `e2e` build tag. Fresh binaries, sandboxed `HOME`/`MEEPT_HOME`, fake LLM. Never touches `~/.meept` or a live daemon. Seconds per suite. Runs in CI (code-quality.yml, job `e2e-fast`) and in the pre-commit hook via the affected-suite gate. |
| **Live-model** | `make e2e-chat` | `scripts/e2e-naive-user-chat.sh` drives a real daemon with a real model through a naive-user chat comparison. Slow, costs tokens, needs a configured local daemon. Local-only by design — never in CI. |

Single suite: `make e2e-fast-area AREA=smoke`.

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

## Manifest format (`e2e/manifest.json`)

Two structures matter to the tooling:

```json
{
  "path_map": {
    "internal/rpc/": ["rpc-ping", "chat-submit", "session-binding"],
    "internal/comm/": ["ws-classification", "ws-filter", "..."]
  },
  "suites": [
    {"name": "smoke", "dir": "e2e/suites/smoke", "status": "implemented"},
    {"name": "rpc-ping", "dir": "e2e/suites/rpc-ping", "status": "todo"}
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
- `scenarios`: the human/agent-readable inventory of what each suite covers
  (id, owning suite, size estimate, one-line title, representative paths).

When you add a package or suite, update this manifest in the same commit.

## Affected script (`scripts/e2e-affected.sh`)

Maps changed paths through the manifest and runs only what's affected:

```bash
scripts/e2e-affected.sh --list                 # print affected suite names only
scripts/e2e-affected.sh internal/rpc/foo.go    # paths as args
git diff --name-only HEAD | scripts/e2e-affected.sh   # paths on stdin
make e2e-affected                              # same, --from-diff
```

Behavior:

- Affected suite dirs that exist → `go test -tags e2e -count=1 -p 2` on them.
- Affected suites still `todo` → reported and skipped.
- No path_map hit but Go files changed → runs the smoke suite as fallback so
  the gate never silently passes.
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
