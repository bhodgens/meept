# Agent Reference: Static Analyzers

Moved from the root `AGENTS.md` to keep the root under the size cap. Update the
root pointer and this file in the same commit.

## Static Analyzers

Custom analyzers in `tools/analyzers/`:

| Analyzer | Purpose | Run |
|----------|---------|-----|
| `mutexio` | Detects I/O under mutex | `make mutexio` |
| `predid` | Detects predictable ID generation | `make predid` |
| `fieldguard` | Guards immutable struct fields | `go run ./tools/analyzers/fieldguard/...` |
| `selflock` | Detects self-deadlocking locks | `go run ./tools/analyzers/selflock/...` |

Audit scripts in `scripts/`:

| Script | Purpose |
|--------|---------|
| `audit-dart-enum-name-shadow.py` | Flags Dart extensions shadowing Enum.name/index |
| `audit-utf8-byte-arithmetic.py` | Flags hand-rolled ASCII case-conversion corrupting UTF-8 |
| `audit-secrets-value-callers.py` | Pins the secrets in-process-only rule: `Broker.Value`/`resolve` callers must be allowlisted; their results never reach logs, env entries, or `os.Setenv` |
| `audit-sqlite-dsn-pragmas.py` | Flags modernc-style `sql.Open("sqlite")` DSNs using mattn keys (`_journal_mode`/`_busy_timeout`/`_fk`/`_foreign_keys`) — modernc.org/sqlite silently drops them (only `_pragma=name(value)` reaches SQLite). bughunt 2026-11-07 infra-fixes |
| `orphan-check.py` | Ratchets unwired bus topics (publisher-without-subscriber and vice versa) from `docs/generated/bus-topology.json` against `scripts/orphan-baseline.txt` — new-only blocks, `--rebaseline` is a deliberate human step for that baseline only. `make orphan-check`; CI step in code-quality.yml generated-artifacts |
| `agents-check.py` | Fails if root `AGENTS.md` exceeds 16 KiB, a path pointer is broken, a required routing pointer is missing, or a NEW top-level package lacks a Key Components row. Baseline: `scripts/agents-baseline.txt` (70 pre-existing omissions); `--rebaseline` is a deliberate human step. Scans the root file plus all 12 routed `docs/agents/*.md` files for broken pointers. Wired in `make agents-check`, pre-commit gate 21, and `code-quality.yml` generated-artifacts. |
| `deadcode-baseline-growth-check.sh` | Fails if `.githooks/deadcode-baseline.txt` or `deadcode-baseline-notest.txt` gained lines versus a base sha. A missing working-tree baseline exits 2 (no empty-file substitute). Wired in ci.yml job `deadcode` after the full-tree reachability gate, and in `.githooks/pre-commit-deadcode` against HEAD before the no-staged-.go return. No env skip. A zero `github.event.before` is not a skip. Pull-request label `deadcode-baseline-growth` is the only exception, and only on a pull request. |
| `gen-connectivity-graph.py` | Generates bus/RPC/HTTP/WS topology (`make graphs`) |
| `fmt-check.sh` | gofmt gate for the Go tree (`make fmt-check` check-only / `make fmt` apply); scope mirrors the `.golangci.yml` `formatters` block (archive/, third_party/, testdata/, vendor/, .worktrees/ + nested modules excluded); a missing gofmt is a HARD ERROR, never a silent pass (issue #138 CI gate) |
| `ensure-dev-key.sh` | Provision the per-installation dev API key (`$MEEPT_HOME/dev_key`, 0600) |
| `gui-daemon-connect.py` | Reconcile the installed daemon config with the GUI connect path (`ensure-config` / `endpoint` / `check` / `probe`) |
| `verify-gui-connect.sh` | Prove a fresh install's GUI reaches the daemon (config, key, TLS pin, WS upgrade) |
| `research-harness-lit.py` | Verifies harness-technique catalog evidence and markdown freshness (`make research-harness-check`) |
| `config-drift-check.py` | Diffs config templates vs the installed `$MEEPT_HOME`; classifies drift via `// drift:` annotations; report-only, exit 1 on unclassified drift |
| `voice-gap-analyze.py` | Measures voice-playback jitter from `meept voice link --record` `.raw` files: silence gaps >=200 ms per side + both-sides talk-over windows (acceptance evidence for the prefetch/turn-taking fixes) |
| `release.sh` | Release pipeline (DRY_RUN=1 default): clean-tree gate, stamped sanity build, `goreleaser` snapshot/publish per `.goreleaser.yml` (Dockerfile + SBOM included) |

All analyzers and audit scripts run via `make lint-ci`.


