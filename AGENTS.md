# AGENTS.md

Guidance for AI coding agents working in this repository.

**Before touching code in a package, read its invariant doc** — the root file keeps only
summaries; the full contracts live next to the code:

```
internal/llm/     -> internal/llm/AGENTS.md     (quota blocks, refusals, alias resolution)
internal/daemon/  -> internal/daemon/AGENTS.md  (runtime lifecycle, spawn guards)
internal/agent/   -> internal/agent/AGENTS.md   (chat contracts, output filters, session IDs)
internal/comm/    -> internal/comm/AGENTS.md    (WS classification, bus topics)
docs/agents/      -> coding-practices.md | opt-in-defaults.md | projects-and-workdirs.md
```

Hermes merges every AGENTS.md on the directory chain from git root to cwd, so the
per-package files load automatically when working inside that directory. Update a rule
and its summary (or its doc) in the same commit.

**This file must be reviewed and validated for completeness and correctness on
every commit.** If a commit adds, removes, or renames a package, changes a build
command, introduces a new convention, or invalidates any statement below, update
this file in the same commit. Stale agent guidance causes more bugs than no
guidance at all.

## Build & Development Commands

```bash
# Build
go build -o bin/meept-daemon ./cmd/meept-daemon
go build -o bin/meept ./cmd/meept
make build              # Everything (daemon + CLI + gendoc + GUI + lite + graphs)
make build-gui          # Flutter GUI only
make menubar-install    # macOS MenuBar app
make deps-llama-check   # Enforce the llama.cpp build floor (>= b9660, LFM2.5 tool-call
                        # parser); wired into `make deps` and `make install`. `make
                        # deps-llama` installs it into $MEEPT_HOME/deps/llama.cpp

# Test
#
# macOS: full-sweep runs MUST bound package parallelism (-p 2). Unbounded
# `go test ./...` / `./internal/...` opens enough concurrent localhost
# sockets to exhaust the ephemeral port range (49152-65535) mid-run, and
# unrelated packages then fail with
#   dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address
# `make test` and friends already pass -p 2 (override: make test
# TEST_PACKAGE_PARALLELISM=N).
go test -p 2 ./... -v
go test -p 2 -race ./...
go test -p 2 ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
make test            # canonical full suite (short mode, -p 2)

# Run
./bin/meept-daemon -f           # Daemon foreground
./bin/meept chat "message"      # CLI chat
./bin/meept chat                # Interactive TUI
agent-tui ./bin/meept chat      # TUI testing

# Config
./bin/meept config              # Interactive editor
./bin/meept config get <key>    # Get value
./bin/meept config set <key> <v> # Set value

# Soul (user-authored persona, ~/.meept/SOUL.md)
./bin/meept soul show           # Current content + sha256 + size
./bin/meept soul path           # Resolved path (honors MEEPT_HOME)
make config-bootstrap           # Copy missing config templates into $MEEPT_HOME
make dev-key                    # Provision $MEEPT_HOME/dev_key (0600), shared with the GUI
make build-gui-dist             # Flutter GUI with NO embedded dev key (distribution;
                                # first-run pairing via the daemon's loopback handshake)
make gui-connect-setup          # Make an installed home GUI-ready (transport.http + dev key)
make gui-connect-check          # Static self-check of the installed GUI connect path

# Agents (AI Employees) — not the dispatcher roster in config/agents/
./bin/meept agents list                 # List employees
./bin/meept agents show <id>            # Full definition
./bin/meept agents create <def.json5>   # Register new employee
./bin/meept agents pause <id> / resume <id>
./bin/meept agents goals [--employee=<id>]
./bin/meept agents set-gate <goal-id> --command="go test -p 2 ./..."
./bin/meept agents audit <id> [--since=6h]

# Plans
./bin/meept plans list/show/approve/reject/confirm <id>

# Projects
./bin/meept projects list/add/remove/sync/status <name>

# Connectivity graphs
make graphs               # Regenerate bus/RPC/HTTP/WS topology (writes docs/generated/*)
# Both freshness checks below run in CI (code-quality.yml, job
# "generated-artifacts"). Run `make graphs` after changing bus/RPC/HTTP/WS
# surfaces; the generator still embeds absolute line offsets (audit F66), so a
# red graphs-check after an unrelated edit means "regenerate", not "topology
# changed".
make graphs-check         # Verify generated files are fresh (CI gate)
make research-harness      # verify harness catalog evidence + regenerate techniques md
make research-harness-check # CI: fail if evidence missing or techniques md stale

# Reference docs (magefiles/docs.go)
make docs-generate        # Regenerate docs/reference/generated/* (needs mage + gomarkdoc)
make docs-check           # Verify those pages are fresh (CI gate; needs mage + gomarkdoc)

# Classifier-eval guards (tools/classifier-eval)
make classifier-eval-selftest # CI: corpus/replay guard + coverage-floor self-test
make e2e-sweep-selftest       # CI: offline sweep grading/completion/CLI regressions

# Git hooks (bash >= 3.2; sub-hooks run under whatever `bash` is on PATH, so the
# suite also executes on the Linux CI runner)
make hooks                # core.hooksPath -> .githooks (17 pre-commit checks)
# Package-classification rule for those hooks: a staged package's state comes
# from `go list -e` STDOUT only. {{.Error}} prints there, while stderr carries
# progress noise ("go: downloading ..." on a cold GOMODCACHE) that must never be
# read as a load failure — folding stderr in (2>&1) blocked healthy commits on
# any machine with an empty module cache. The classifier detects LOAD failures
# and nothing else: `broken` is `go list` exiting non-zero with an empty stdout
# (stderr is captured separately for the reason) or a non-empty {{.Error}} that
# matches none of the exclusion reasons. A SYNTAX ERROR or an UNRESOLVED IMPORT
# is NOT `broken`: both give rc=0 with an EMPTY .Error, so the package classifies
# `ok` and surfaces as a go vet / staticcheck finding instead (go build, for
# pre-commit-build). A directory with no (non-test) Go files reports `nofiles` —
# a SKIP, not a failure; that is a deliberate loosening (the pre-68b2338c gate
# failed there).
# pre-commit-build's root-artifact check blocks a new root-level entry only when
# a build can create it: the gitignored binaries (meept, meept-daemon, meept-lite,
# llmdoc — including a stale one a build clobbers), `*.test`, or the basename of
# any `package main` directory in the module (a bare `go build ./cmd/gendoc`
# writes /gendoc), derived from `go list` rather than a fixed list. Any other new
# root-level path is reported, never blocking.

# Static analyzers
make analyzers            # mutexio + predid
make lint-ci              # golangci-lint + analyzers + audit scripts + fmt-check-gui
                          # (fmt-check-gui hard-requires the Dart SDK / Flutter)
```

See `cmd/meept/`, `cmd/meept-daemon/`, and `Makefile` for full command reference.

## Architecture Overview

Meept is a **Go platform** with skill-based task orchestration, LLM integration,
memory management, and external integrations.

### Request Flow

```
User Input → CommServer (RPC/HTTP) → MessageBus → AgentLoop → Dispatcher → Planner → Tools → Response
```

### Key Components

| Layer | Packages |
|-------|----------|
| **Server** | `cmd/meept-daemon`, `internal/daemon`, `internal/rpc`, `internal/bus` (typed topics: `bus.Topic[T]`/`PublishT`/`SubscribeT`, `internal/bus/topic.go`), `internal/comm` (WS classification: `internal/comm/wsclass`) |
| **Agent** | `internal/agent` (loop, orchestrator, planner, collaborative, workspace, executor, dispatcher) |
| **LLM** | `internal/llm` (client, resolver, budget, providers, token cache, context firewall) |
| **Memory** | `internal/memory` (manager, episodic, task, ftstore, facts) |
| **Tools** | `internal/tools` (registry, builtin/*, mcp), `internal/acp` (ACP client wire) |
| **Security** | `internal/security` (engine, sanitizer, tirith, tls, fence), `internal/auth` (multi-user store: users/keys/expiry, quota+permission stubs) |
| **Employee** | `internal/employee` (constitution, goal, goal_loop, enforcement, authority, manager) |
| **Audit Chain** | `internal/auditlog` (canonical JSON, hash chain, store, anchoring, verification) |
| **Session** | `internal/session` (store, store_sqlite, threads, messages) |
| **Effects** | `internal/effects` (external-effect idempotency ledger) |
| **Services** | `internal/services` (chat, session, terminal, push, reflection) |
| **Project** | `internal/project` (manager, init_deep, detection) |
| **TUI** | `internal/tui` (app, commands, components, handlers, modals, models, tableutil) |
| **GUI** | `ui/flutter_ui` (Flutter web + desktop) |
| **Scheduling** | `internal/scheduler`, `internal/queue`, `internal/worker` |
| **Skills** | `internal/skills`, `internal/selfimprove` |
| **Infra** | `internal/config`, `internal/metrics`, `internal/transport`, `internal/pty`, `internal/eval`, `internal/gate` |

See `docs/concepts/architecture.md` for full documentation.

### Connectivity Graph (Bus / RPC / HTTP / WS)

The daemon's components communicate via string-typed bus topics, RPC handlers,
and HTTP routes — all invisible to the compiler. A generated connectivity graph
maps every publish/subscribe edge so you can trace cross-boundary data flow
without reading the code:

- **`docs/generated/bus-topology.md`** — human-readable: every bus topic with
  publishers, subscribers, payload keys; orphan analysis; WS event
  classification; RPC handler map; HTTP route map.
- **`docs/generated/bus-topology.json`** — machine-readable version.
- **`docs/generated/rpc-handlers.json`**, **`http-routes.json`**,
  **`ws-event-map.json`** — individual layer exports.

Regenerate with `make graphs` (runs automatically on every `make build`).

**When debugging cross-boundary issues** (events not reaching clients, wrong
payload fields, orphaned listeners), start with `docs/generated/bus-topology.md`
before reading source.

## Critical Invariants

### Chat replies must be honest and user-shaped

The chat path is the user's only window into the daemon. Full contracts (async turns,
errored steps, per-session working dirs, no global active-project fallback,
machine-shaped-output guard) live in `internal/agent/AGENTS.md`. Short form: turns are
async, errored steps never pass review, and projects bind PER SESSION with no global
fallback. `session_id` and `conversation_id` are distinct; new code handles both.

### Projects, working dirs, and trust boundaries

Full rules live in `docs/agents/projects-and-workdirs.md`. Short form: never use
`os.Getwd()` as a fallback in `internal/`/`pkg/` daemon code; projects bind PER
SESSION with no global active-project fallback; `multiuser.enabled=false` preserves
the legacy flat-key HTTP path byte-identically; Unix RPC stays owner-trusted (no
token auth on the socket).

### First-run pairing and the dev key

The per-installation dev key ($MEEPT_HOME/dev_key) is generated daemon-side at
first boot; it must NEVER be baked into a distributed GUI binary. `make
build-gui-dist` builds with no embedded key; a key-less GUI pairs on first run
through the daemon's loopback-only handshake (`internal/comm/http/pairing.go`):
a single-use `crypto/rand` code printed to the daemon console, exchanged once
at `POST /api/v1/pair/exchange`. Pairing arms ONLY when require_auth is on with
no explicit api_keys and no auth store. Never log the dev key or pairing code
at info level; the build-time dart-define path (`make build-gui`/`devbuild`)
remains dev-workflow-only.

### WS event classification and bus topics

Full rules live in `internal/comm/AGENTS.md`. Short form: only `chat_message` topics
produce `type: "chat_message"` (everything else is `agent_progress`); `chat.response`
is never WS-relayed; typed `bus.Topic[T]` for stable payloads, raw `Publish` for
multi-shape; a bus proxy registration needs a live responder on both sides.

### Quota errors are not failures (quota-reset-resilience)

Full invariants live in `internal/llm/AGENTS.md` (quota blocks, endpoint cooldowns,
refusals, alias resolution, universal parking, slot gate). Short form: a
`*llm.QuotaResetError` is never an alias failure and never short-retried; check
`ErrAllModelsQuotaBlocked` / `ErrAllEndpointsBlocked` with `errors.Is`.

### Opt-in defaults and evolver wiring

Full rules live in `docs/agents/opt-in-defaults.md`. Short form: `skills.state`,
`plans.parallel_phases`, and `plans.plan_compiler_enabled` are opt-in with default
false; flipping a default is a product decision. Wiki/trace stores are evolver-only,
never reachable from inference-path prompt builders; wire trace writer + usage
tracker + learning pipeline on every new loop; construct the evolver after its
dependencies in daemon startup.

### Local runtime lifecycle: spawn guards and orphan reaping

Full rules live in `internal/daemon/AGENTS.md`. Short form: runtimes are long-lived
daemon children — never spawn into a served endpoint, never kill another instance's
runtime, only `StartupOrphanSweep` reaps orphans, sweep before spawn, and a local
classifier that never becomes healthy fails startup (F-D8).

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
  `scripts/e2e-affected.sh`). The fast tier is hermetic and runs in CI;
  `make e2e-chat` is the live-model tier, local-only.
- Enforcement: pre-commit check [18/18] (`pre-commit-e2e`) runs the affected
  suites on staged `internal/|pkg/|cmd/` Go changes and blocks commits that
  add files under a NEW package directory without an e2e suite + manifest
  entry. Emergency-only bypass: `MEEPT_SKIP_E2E=1 git commit` (prints a loud
  warning). When you add a package, add its `path_map` entry + suite to the
  manifest in the same commit.

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

## UI Conventions

- **All UI text must be lowercase** (e.g., "switch" not "Switch", "ok" not "OK")
- For TUI, use bubblezone for positioning
- Default to clickable elements for context switching
- **TUI and Flutter GUI features must be kept at parity.** When a feature is
  added or changed in one surface, the other surface gets the same capability.
  This includes: status bar elements, command palette items, keyboard shortcuts
  (prefer identical keys across surfaces — e.g., `Ctrl+V` for verbosity on all
  platforms, not `Cmd+V` on mac), session/agent/tab semantics (e.g., archive vs
  delete), and tab affordances. Document surface-specific deviations explicitly
  with a justification.
- **TUI tables are sized on both axes.** `bubbles/table` owns a viewport that
  starts at width 0, and a width-0 viewport renders no lines: a table given only
  `SetHeight` draws its header over an empty body while cursor navigation still
  works. Size every table through `internal/tui/tableutil.Size` and write rows
  through `tableutil.SetRows`, which normalizes each row to the current column
  count (a row longer than the column list panics inside `bubbles/table`). A
  column change clears the rows first (`SetColumns` re-renders them) and
  repopulates from cache afterwards. See `docs/workflows/tui.md`.

## Flutter Multi-Platform (Web + Desktop)

When modifying Flutter UI code, ensure web compatibility alongside desktop
(macOS/Linux/Windows):

- **Avoid top-level `dart:io` imports in shared code** — use `kIsWeb` guards
  or conditional imports
- **Platform detection:** use `bool.fromEnvironment('dart.library.io')` for
  compile-time checks, `Platform.isMacOS` only in `!kIsWeb` guarded code
- **File I/O:** wrap in `if (kIsWeb) return;` guards; web uses file pickers,
  not direct paths
- **Platform abstraction:** for shared platform abstractions, use a singleton
  service pattern (e.g., `PlatformService`) that provides safe null/default
  returns on web

**See also:** `ui/flutter_ui/lib/core/platform/platform_service.dart` and
`platform_native_helpers.dart` for the platform abstraction layer pattern.

## Configuration

All config uses **JSON5** format. Templates in `config/`, copied on
`make install`.

- **Main**: `~/.meept/meept.json5`
- **Models**: `config/models.json5` (capability-based resolution)
- **MCP servers**: `~/.meept/mcp_servers.json5` (22 preconfigured, 7 enabled
  by default — incl. `obscura` browser MCP, enabled; `excel` xlsx fallback,
  disabled). Every stdio entry carries `install_hint`; `meept doctor`
  surfaces it and `meept doctor --fix --install-missing` runs hints
  opt-in (consent per command). Daemon/launchd/menubar launches augment
  PATH via `internal/daemon/daemonpath.go` `DaemonPath()` (menubar keeps
  a synced Swift copy) — keep new guaranteed dirs in both.
- **Skills**: `config/skills/` ships via `make install` / `make sync-config`
  into `~/.meept/skills/` (merge, no-clobber: user-modified files are kept,
  new defaults land as `<file>.new` — see `scripts/install-sync.py`).
  Frontmatter `requires-tools:` gates
  execution on tool availability (see docs/workflows/skills.md).
- **Home dir**: everything meept reads/writes lives under one home —
  `$MEEPT_HOME` when the env var is set, else `~/.meept`
  (`config.MeeptHome()` / `MeeptPath()` in internal/config/home.go are THE
  resolution points; config paths carrying the `~/.meept` prefix are
  redirected through it too). Set `MEEPT_HOME` consistently for daemon,
  CLI, and `make sync-config` — they all honor it.
- **ACP agents**: `~/.meept/acp_agents.json5` (catalog of external ACP
  agents; `[acp] enabled` defaults false — no subprocesses until opted in)
- **UI theme** (TUI + GUI): shared tokens in `theme/tokens.json5`; select per
  client via `rendering.ui_theme` — see `docs/configuration/theming.md`.
- **Client**: `~/.meept/client.json5` (TUI keybindings)
- **Log level**: `log_level` field in `~/.meept/meept.json5` (NOT env vars)

See `docs/configuration/` for full reference.

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
| `gen-connectivity-graph.py` | Generates bus/RPC/HTTP/WS topology (`make graphs`) |
| `ensure-dev-key.sh` | Provision the per-installation dev API key (`$MEEPT_HOME/dev_key`, 0600) |
| `gui-daemon-connect.py` | Reconcile the installed daemon config with the GUI connect path (`ensure-config` / `endpoint` / `check` / `probe`) |
| `verify-gui-connect.sh` | Prove a fresh install's GUI reaches the daemon (config, key, TLS pin, WS upgrade) |
| `research-harness-lit.py` | Verifies harness-technique catalog evidence and markdown freshness (`make research-harness-check`) |

All analyzers and audit scripts run via `make lint-ci`.

## Feature Documentation Requirements

All code changes to feature implementations must have corresponding
documentation updates.

**Documentation locations:**
- `docs/workflows/` — Feature specifications
- `docs/concepts/` — Architecture
- `docs/reference/` — CLI, API, tools
- `docs/research/` — living research maps (harness techniques)
- `docs/generated/` — Auto-generated connectivity graphs (do not edit)

**Feature mapping:** `internal/<pkg>/` → `docs/workflows/<pkg>.md`

## AGENTS.md Maintenance Rule

**Every commit MUST review this file for completeness and correctness.**

Before committing, verify:
1. **Package table** — any added/removed/renamed packages in `internal/`,
   `cmd/`, or `pkg/` are reflected in the Key Components table.
2. **Build commands** — new Makefile targets or changed build steps are listed.
3. **Invariants** — new cross-boundary contracts (bus topics, session ID
   semantics, WS event types) are documented under Critical Invariants.
4. **Analyzers/scripts** — new static analyzers or audit scripts are listed.
5. **Conventions** — new coding conventions discovered during the change are
   captured.
6. **Invariant docs** — a rule whose full text lives in a package
   `AGENTS.md` (`internal/{llm,daemon,agent,comm}/AGENTS.md`) or a
   `docs/agents/*.md` doc was updated in the same commit as its summary
   here.

If any item is stale, fix it in the same commit. Do not defer AGENTS.md
updates to a follow-up.
