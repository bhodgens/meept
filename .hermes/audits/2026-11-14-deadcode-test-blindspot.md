# deadcode `-test` blind-spot census (2026-11-14)

Tool: `/Users/caimlas/go/bin/deadcode` (golang.org/x/tools v0.52.0 per `go.mod`
require line), `GOTOOLCHAIN=go1.27.3` (pinned from `go.mod` toolchain line),
`LC_ALL=C`. Analysis identical to `.githooks/pre-commit-deadcode`: per-main
unreachable set → normalize (strip `:line:col:`) → `sort -u` → incremental
`comm -12` intersection across the five production mains
(`./cmd/meept-daemon ./cmd/meept ./cmd/meept-lite ./cmd/meept-classifier-test
./tools/bridges-eval`).

Working-tree state at measurement: main tip `1598c095` plus uncommitted
sibling-session edits (raw outputs are disk-tree reads; see "caveats").

## Runtime: `-test` vs no-`-test` (seconds, rc for both = 0)

| main                     | with `-test` | without `-test` |
|--------------------------|-------------:|----------------:|
| ./cmd/meept-daemon       |           5s |              4s |
| ./cmd/meept              |           4s |              5s |
| ./cmd/meept-lite         |           4s |              4s |
| ./cmd/meept-classifier-test |        3s |              3s |
| ./tools/bridges-eval     |           4s |              3s |

No material runtime difference — the second (no-`-test`) pass doubles total
gate wall time (~17s → ~34s warm-cache on this 24-core Apple Silicon host;
cold build cache adds the one-time package-load cost) but each main stays
seconds-scale. Both passes fit comfortably in the 15-minute CI job timeout.

## Unreachable-set sizes (working tree, normalized, per main)

| main          | `-test` | no-`-test` |
|---------------|--------:|-----------:|
| daemon        |  2318   | 2318       |
| cli (meept)   | 12433   | 12448      |
| lite          | 12051   | 12051      |
| classifiertest|  7854   |  7854      |
| bridgeseval   |  8067   | 8067       |
| **intersection** | **534** | **536**  |

## Blind-spot definition

A function is a `-test` blind spot when it appears in the **no-test
intersection** (unreachable from all five mains without test roots) but NOT
in the **`-test` intersection** — i.e. some main's test binary makes it look
reachable. This is exactly the "AI writes the function + its unit test, never
wires it" shape.

**Gate-relevant blind-spot count: 2** (comm -23 inter.notest inter.test):

| function | declared at | only callers |
|----------|-------------|--------------|
| `agent.LaneAgentFor` | `internal/agent/llm_classifier.go:161` | `cmd/meept/lanes_test.go:54` (a test in the *cli* main — which is why the blind spot only shows once intersected) |
| `config.DefaultPeerSyncConfig` | `internal/config/sync_config.go:24` | `cmd/meept/sync_cmd_test.go:404`, `internal/daemon/sync_wiring_test.go:104` |

Neither appears in any non-test source call path. Both were invisible to the
old `-test`-only gate and are the only two additions carried by the new
`.githooks/deadcode-baseline-notest.txt` (503 = 501 committed baseline + 2).

## Per-binary notest-only diff (functions whose reachability flips when test
roots are removed, any single main): **16, all in `./cmd/meept`** — other four
mains show zero.

### Package clusters (cli main)

- **`cmd/meept/config_migrate_reasoning.go` — 9 funcs** (`atomicWriteFile`,
  `discoverAgentFiles`, `ensureMappingNode`, `injectReasoningBlock`,
  `kindName`, `renderReasoningBlock`, `scalarNode`, `setMappingField`,
  `splitAgentFrontmatter`): the whole config-migrate-reasoning command's
  helpers. These 9 do NOT reach the intersection blind-spot list because the
  command's *entry* functions are dead in the `-test` intersection too
  (nothing even tests the cmd); the test-root effect only rescues them in the
  cli set.
- **`cmd/meept/doctor_providers.go` — 1** (`sortProviderCheckNames`),
  **`cmd/meept/oauth.go` — 1** (`readPastedCode`): helpers only unit tests call.
- **`internal/agent/llm_classifier.go` — 1** (`LaneAgentFor`) — intersection blind spot.
- **`internal/auth/store.go` — 1** (`Store.Validate`).
- **`internal/config/sync_config.go` — 1** (`DefaultPeerSyncConfig`) — intersection blind spot.
- **`internal/llm/reasoning.go` — 1** (`IsValidEffort`).
- **`internal/tui/types/types.go` — 1** (`TokenBudget.Ratio`).

Cluster shape: 11 of 16 are `cmd/meept`-local helpers whose only caller is a
test in the same package; the remaining 5 are `internal/` API functions kept
"alive" solely by white-box tests in `cmd/meept` or `internal/daemon`.

## Why the census is small at intersection level (and why the pass still matters)

RTA treats a test file's calls as real edges, so a function tested-but-never
wired is rescued per-binary; but the *intersection* only hides such a function
if the rescue happens consistently. The two census functions prove the rescue
is real and gate-relevant, and the cost of the second pass is ~17s. A future
unwired function rescued by its own test in any main is caught the moment its
callers exist in no other production path — exactly when the commit lands.

## Caveats

- Raw sets above are **working-tree** reads; uncommitted sibling edits are
  included (e.g. `internal/kanban/*`, `internal/daemon/kanban_boot.go` untracked).
- The committed `-test` baselines (501) were NOT regenerated at HEAD because
  `deadcode -test ./cmd/meept` **fails at the committed tip**:
  `cmd/meept/voice_transcript_bounded_test.go` (committed) references
  `transcriptReadTimeout`, which lives only in the sibling's *uncommitted*
  `cmd/meept/voice.go`. The hardened gate fail-closes on this (correct
  behavior — the tip cannot typecheck with tests). The no-`-test` pass runs
  clean at HEAD (rc=0, intersection 503) — the baseline committed here is that
  HEAD no-test intersection, sorted `LC_ALL=C`.
- Working tree vs committed baselines currently shows 33–35 "newly unwired"
  entries (31 `internal/kanban` + 2 `internal/validator`) — sibling sessions'
  in-flight unwired kanban surface. That is the gate correctly firing on code
  that exists in the worktree but is unreachable from all five mains.

## Reproduce

```bash
cd <meept>   # any checkout of 1598c095 for the 503 baseline
export GOTOOLCHAIN=go1.27.3 LC_ALL=C
DC=/Users/caimlas/go/bin/deadcode
for m in ./cmd/meept-daemon ./cmd/meept ./cmd/meept-lite ./cmd/meept-classifier-test ./tools/bridges-eval; do
  $DC "$m" | sed -E 's/:[0-9]+:[0-9]+: unreachable func:/ unreachable func:/' | sort -u
done | ...   # comm -12 incremental intersection == .githooks/pre-commit-deadcode --rebaseline
```
