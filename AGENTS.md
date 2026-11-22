# AGENTS.md

Guidance for AI coding agents working in this repository. Keep this file under
16 KiB. Global hard rules live here; full text lives in the referenced docs.
Update a rule and its pointer in the same commit.

## Invariant doc routing table

**Before touching code in a package, read its invariant doc.** Hermes merges
every AGENTS.md on the directory chain from git root to cwd, so per-package
files load automatically when working inside that directory.

```
internal/llm/       -> internal/llm/AGENTS.md
internal/session/   -> internal/session/AGENTS.md
internal/daemon/    -> internal/daemon/AGENTS.md
internal/agent/     -> internal/agent/AGENTS.md
internal/comm/      -> internal/comm/AGENTS.md
internal/kanban/    -> internal/kanban/AGENTS.md
internal/kanbansvc/ -> internal/kanbansvc/AGENTS.md
internal/voice/     -> internal/voice/AGENTS.md
docs/agents/        -> commands.md | architecture-overview.md | configuration.md |
                       static-analyzers.md | invariants.md | ui-and-feature-rules.md |
                       coding-practices.md | coding-practices-extra.md |
                       opt-in-defaults.md | projects-and-workdirs.md |
                       voice-invariants.md (load only for voice work)
```

The voice sidecar contract is split out: read `docs/agents/voice-invariants.md`
only for voice work. User-facing map: `docs/workflows/voice-sidecar.md`.

## Build and Test Commands

Full command list: `docs/agents/commands.md`, `.githooks/README.md`. Hard rules:

- Build: `make build` (daemon, CLI, voice child, GUI, lite, graphs).
- Test: `make test`; any full sweep MUST use `-p 2` on macOS (port exhaustion
  otherwise).
- Run: `./bin/meept-daemon -f`, `./bin/meept chat`, `./bin/meept agents ...`.
- Hooks: `make hooks` installs 20 pre-commit checks + pre-push e2e/bench gates.

## Architecture

Full component map and request flow: `docs/concepts/architecture.md`,
`docs/agents/architecture-overview.md`. Generated bus/RPC/HTTP/WS topology:
`docs/generated/bus-topology.md`. Start there for cross-boundary bugs.

### Key Components

| Layer | Packages | Contract |
|-------|----------|----------|
| Server | `internal/daemon`, `internal/rpc`, `internal/bus`, `internal/comm`, `cmd/meept-daemon` | `internal/daemon/AGENTS.md`, `internal/comm/AGENTS.md` |
| Agent | `internal/agent` | `internal/agent/AGENTS.md` |
| LLM | `internal/llm`, `internal/llm/warm.go` | `internal/llm/AGENTS.md`, `docs/workflows/llm-routing-quality.md` |
| Memory | `internal/memory` | — |
| Tools | `internal/tools`, `internal/acp`, `internal/hermes` | `docs/workflows/github.md` |
| Codemode | `internal/codemode` | `docs/workflows/codemode.md` |
| Security | `internal/security`, `internal/auth`, `pkg/security` | — |
| Employee | `internal/employee` | — |
| Audit chain | `internal/auditlog` | — |
| Session | `internal/session` | `internal/session/AGENTS.md` |
| Effects | `internal/effects` | — |
| GitHub | `internal/githubsvc`, github_* tools in `internal/tools/builtin` | `docs/workflows/github.md` |
| Intake | `internal/intake`, `internal/rpc/intake_status.go` | `docs/workflows/intake.md` |
| Kanban | `internal/kanban`, `internal/kanbansvc`, `cmd/meept-kanban` | `internal/kanban/AGENTS.md`, `internal/kanbansvc/AGENTS.md`, `docs/workflows/kanban.md` |
| SMS leg | `internal/sms`, `internal/telnyx`, `internal/voice/telnyx`, `internal/comm/http/messaging.go` | `docs/workflows/sms.md` |
| Project | `internal/project` | `docs/agents/projects-and-workdirs.md` |
| Voice | `internal/voice`, `internal/voiceframe`, `internal/voicechild`, `cmd/meept-voice` | `docs/agents/voice-invariants.md` (load only for voice work), `internal/voice/AGENTS.md` |
| TUI / GUI | `internal/tui`, `ui/flutter_ui` | `docs/workflows/tui.md`, `docs/workflows/flutter_gui.md` |
| Scheduling | `internal/scheduler`, `internal/queue`, `internal/worker` | — |
| Skills | `internal/skills`, `internal/selfimprove` | — |
| Infra | `internal/config`, `internal/metrics`, `internal/transport`, `internal/pty`, `internal/process`, `internal/eval`, `internal/gate`, `internal/langcode` | `docs/workflows/langcode.md` |

New package? Add a row in the same commit. `make agents-check` enforces this.

## Critical Invariants

Full text and rationale: `docs/agents/invariants.md`. Hard red lines only here:

- Chat: turns are async, errored steps never pass review, projects bind per
  session, `session_id` and `conversation_id` differ. `internal/agent/AGENTS.md`.
- Projects: no `os.Getwd()` fallback in `internal/` or `pkg/` daemon code;
  Unix RPC is owner-trusted. `docs/agents/projects-and-workdirs.md`.
- Dev key: `$MEEPT_HOME/dev_key` is generated daemon-side; NEVER bake into a
  distributed GUI; never log key or pairing code. `internal/comm/AGENTS.md`.
- WS: only `chat_message` topics emit `type: "chat_message"`; `chat.response`
  is never relayed; kanban events classify as `board_event`.
  `internal/comm/AGENTS.md`.
- Quota: a `*llm.QuotaResetError` is never an alias failure; empty completion is
  an alias failure on every client. `internal/llm/AGENTS.md`.
- Opt-in: `skills.state`, `plans.parallel_phases`, and
  `plans.plan_compiler_enabled` default false; kanban defaults true.
  `docs/agents/opt-in-defaults.md`.
- Runtime: long-lived daemon children only; `StartupOrphanSweep` alone reaps;
  transports start before the F-D8 gate. `internal/daemon/AGENTS.md`.
- Affinity: headers default on; empty scope sends NO header; teardown is
  best-effort and never fails a turn. `internal/llm/AGENTS.md`.
- LLM routing: posture -> quality -> admission; quota > endpoint > alias >
  sticky; posture is a compliance boundary; admission is non-retryable;
  everything opt-in. `docs/workflows/llm-routing-quality.md`.
- Voice: load `docs/agents/voice-invariants.md` only for voice work.
- GitHub: `builtin` imports `githubsvc`, never the reverse; token material is
  never logged. `docs/workflows/github.md`.

## Coding Practices

Full rules, code examples, and enforcement details:
`docs/agents/coding-practices.md` and `docs/agents/coding-practices-extra.md`.
Hard rules only here:

- All new feature tests go in the hermetic e2e tier. No new unit test files
  for new features. `docs/workflows/e2e-testing.md`.
- Never use `time.Now().UnixNano()` or `math/rand` for IDs; use
  `pkg/id.Generate()` on entropy failure.
- Prefer early optimization when cost/benefit is reasonable.
- Every feature needs at least one user surface (CLI, TUI, GUI, or HTTP) and
  tests. Internal-only code is incomplete.
- Guard typed-nil interfaces; nil-guard every `Set*`; no I/O under mutex; no
  ignored errors or bare `panic(err)`; two-value map assertions.
- Surface unacted observations at the end of every turn, with `file:line`,
  what, and why not acted on.
- Fix root causes; never ship a workaround as the final solution.

## UI and Flutter Conventions

Full rules: `docs/agents/ui-and-feature-rules.md`, `docs/workflows/tui.md`,
`docs/workflows/flutter_gui.md`. Hard rules only here:

- All UI text is lowercase. TUI uses bubblezone; default to clickable elements.
- TUI and Flutter GUI stay at parity: same status elements, commands, keys,
  session semantics. Document deviations with a reason.
- Size every TUI table on both axes via `internal/tui/tableutil.Size` and
  `tableutil.SetRows`.
- Flutter shared code avoids top-level `dart:io`; guard with `kIsWeb`.
- Feature code changes need a doc update: `docs/workflows/<pkg>.md`.

## Configuration

All config is JSON5. Full paths and defaults: `docs/agents/configuration.md`,
`docs/configuration/`. Hard rules:

- Home: `$MEEPT_HOME` else `~/.meept`. Set it for daemon, CLI, and
  `make sync-config`.
- Main config: `~/.meept/meept.json5`; log level is a config field, not an env var.
- Kanban and models ship default-on in `config/meept.json5` and
  `config/models.json5`; see `docs/agents/configuration.md`.

## Static Analyzers

Analyzer and audit script inventory with run commands:
`docs/agents/static-analyzers.md` (includes `agents-check.py`, the AGENTS.md
size/pointer/package-table ratchet; `make agents-check`). Run via `make lint-ci`.

## AGENTS.md Maintenance Rule

The root file stays under 16 KiB. It holds global hard rules and pointers. A new
rule goes in the owning doc; the root gets a one-line pointer. `make
agents-check` fails the build if the size, the routing table, a broken pointer,
or a new undocumented package breaks.

Run `make agents-check` before committing. Do not defer pointer fixes.
