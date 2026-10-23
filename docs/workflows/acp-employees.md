---
title: ACP Employees (slaved external agents as agent definitions)
status: phase-1 partially landed (persona lane bdc1ae52, field pipeline d47eba12); ACPExecutor branch + wiring pending
decided: 2026-10-22
---

# ACP Employees

## Problem

Meept can drive external coding agents over ACP (see acp.md), but today they
are reachable only through the `acp_agent` tool: an ad-hoc verb the main LLM
must choose to call, with no roster identity. The user wants named, slaved
agents: "have bob check that issue" should route to bob, a hermes agent with
his own profile, exactly like any other agent definition.

## Decisions (2026-10-22)

| Dim | Pick | Summary |
|-----|------|---------|
| 1 | A | Bob is an agent definition (config/agents/bob/AGENT.md), not an employee |
| 2 | B | Multi-session per catalog agent: N personas share one catalog entry |
| 3 | A | Executor branch on AgentSpec: `executor: acp` swaps the LLM loop for an ACP driver |
| 4 | A | Per-agent permission_mode override in frontmatter, global [acp] as fallback |
| 5 | verified | Hermes profiles select via `-p` argv flag: `hermes -p borg acp` (live-verified, hermes 0.21.2) |

## Design

### Agent definition shape

config/agents/bob/AGENT.md frontmatter gains:

```yaml
id: bob
name: bob
role: executor
executor: acp
acp:
  agent: hermes          # catalog id in acp_agents.json5
  persona: bob           # session key; multi-session lane (dim 2B)
  command_args: ["-p", "borg"]   # argv appended after the catalog command
  permission_mode: human # optional override of [acp] permission_mode
  decision_threshold: 0.7 # optional, only with permission_mode: decision
```

The catalog entry stays generic (one `hermes` entry, command
`["hermes", "acp"]`); personas are defined by the agent definitions that
reference it, each injecting its own `command_args` (the `-p <profile>`
flag) and its own permission lane.

### Field pipeline (5 steps, known pitfall)

1. `AgentMetadata` (internal/agents/models.go): add `Executor string` and
   `ACP *ACPAgentMetadata` (agent, persona, command_args, permission_mode,
   decision_threshold). Primary YAML tags are underscore form; camelCase
   variants need the merge lane in parser.go step 2.
2. `parseMetadata` altMeta merge for camelCase-form keys (landed reality:
   primary tags are underscore form; altMeta/camelMeta merges make
   `commandArgs`/`permissionMode`/`decisionThreshold` work too — yaml.v3
   does not match camel keys against underscore tags).
3. `AgentSpec` (internal/agent/spec.go): mirror fields.
4. `definitionToSpec` (internal/agent/registry.go): map metadata -> spec.
5. `mergeSpec` (internal/agent/registry.go): merge logic.

Skipping any step silently drops the field.

### Multi-session Manager lane (dim 2B)

Today `Manager.GetOrCreate(ctx, agentID, workdir)` keys `m.sessions` and
`m.pending` by catalog agentID only (internal/acp/manager.go), one session
per catalog entry, and `SessionConfig` carries no persona.

Changes:

- New method `GetOrCreatePersona(ctx, agentID, persona string, opts
  PersonaOpts) (*Session, error)`. `PersonaOpts` carries CommandArgs
  (appended to entry.Command), PermissionMode override, DecisionThreshold
  override, Cwd override.
- Session key becomes `agentID + "\x00" + persona` in `m.sessions` and
  `m.pending`. `GetOrCreate` (no persona) keeps working: it delegates with
  persona "" and the key `agentID + "\x00"`, so existing callers and tests
  see identical behavior.
- `MaxAgents` counts persona sessions (it already counts map entries; the
  semantics widen — document it).
- `SessionConfig` gains `Persona string` and `CommandArgs []string`;
  spawn concatenates `Command + CommandArgs`.
- Relay attribution (internal/acp/relay.go): `PendingPermission.AgentID`
  stays the catalog id; add `Persona string` so the human relay UI shows
  "hermes/bob", not "hermes". `renderDecisionState` includes the persona.
- Bus/WS: session events gain the persona field; existing consumers ignore
  unknown fields.

Backward-compat rule: every permissive/deny SessionConfig without a persona
stays byte-identical (same pattern as the decision/human lane stamps).

### Executor branch (dim 3A)

In the agent loop seam (internal/agent): when `spec.Executor == "acp"`,
route the turn to a new `ACPExecutor` instead of `reasoningCycle`:

1. Resolve catalog entry via the daemon-held `*acp.Manager`
   (`GetOrCreatePersona`).
2. `session/new` (or reuse the persona's live session), then
   `session/prompt` with the user text.
3. Stream `session/update` notifications onto the existing bus progress
   topics (agent_progress), tagged with the agent id + persona.
4. Final agent message text passes the SAME output path as LLM replies:
   applyReplyGuard, StripPromptScaffolding, output filter chain. Machine-
   shaped output from an external harness must not become a raw reply.
5. Turn bookkeeping (async turns, turn.terminal events, errored-step
   review gating) is unchanged — the ACP turn is just a different producer
   of the terminal result.

The `acp_agent` tool stays for ad-hoc drives; it and ACPExecutor share the
Manager, so a persona session is reused across both paths.

### Permission modes (dim 4A)

Precedence: agent-definition `acp.permission_mode` > `[acp]
permission_mode`. The override flows through `PersonaOpts` into
`SessionConfig.PermissionMode`; the decision/relay lane stamps follow the
existing conditional-stamp rule (decision seam only in decision mode,
relay only in human mode or decision+escalate).

### Dispatcher routing

"have bob check that issue" routes by agent name — the dispatcher already
name-matches roster agents (keywordPatterns + classifier). No new intent
type. Bob appears in `agents list`, the TUI roster, and the GUI agents tab
with no surface changes, because he IS an agent definition. The only
surface addition: `agents show bob` renders an `Executor:` section (acp,
catalog agent, persona, permission mode) — lowercase, following the
Verification section pattern.

## Security

- Attack surface: each persona is a full external agent subprocess with its
  own tool execution. The fence does NOT constrain the subprocess — ACP
  agents execute in their own process; meept's only control points are the
  permission lane (requestPermission) and the spawn cwd. Therefore:
  default_mode ships "" (agent default) but the spec recommends
  "read-only" for exploration personas.
- command_args from frontmatter is argv injection surface: validate at
  parse time — reject args starting with "-" that the target binary does
  not whitelist? No: too brittle. Instead: command_args is operator-authored
  config (same trust class as `command` in acp_agents.json5), NOT
  model-writable. Document that agent definitions are operator-only files.
- Kill switch unchanged: `[acp] enabled = false` disables every persona;
  per-entry `enabled: false` disables a catalog agent and every persona on
  it.
- Persona cap: MaxAgents now bounds total persona sessions; a runaway
  roster of 50 bobs hits ErrMaxAgents, not fork-bomb.

## Implementation phases

- Phase 1 (core): Manager persona lane (GetOrCreatePersona, session key,
  SessionConfig.Persona/CommandArgs, relay persona field) + field pipeline
  (5 steps) + ACPExecutor branch + `agents show` executor section. Ships
  "have bob check that issue" end-to-end with the existing catalog.
- Phase 2 (hardening): per-persona metrics tags (employee.invocations-style:
  acp.persona.turns {agent, persona, outcome}), TUI/GUI persona display in
  progress events, e2e suite (hermetic: fakeagent testdata extended with a
  persona-capable fake).
- Phase 3 (compose): employee layer references ACP-backed agents as
  GoalLoop executors (WithExecutor seam already exists) — autonomous
  hermes-persona employees. Deferred; needs its own design pass.

## Open questions

- Should a persona session idle-reap? The session lifecycle hooks
  (internal/session) reap idle sessions; ACP persona subprocesses are
  long-lived children. Proposal: reap on the same idle timer, respawn on
  next prompt (session/load if the harness supports it — pi-acp and cline
  do; hermes acp unverified).
- Persona name collisions across catalog agents (bob-on-hermes vs
  bob-on-claude): the composite key handles it; the roster shows
  "bob (hermes)" disambiguation.

## Evidence

- hermes 0.21.2: `hermes acp --help` (ACP server mode, --check/--setup),
  `hermes profile alias` generates `exec hermes -p <profile> "$@"`,
  `borg acp --version` -> 0.21.2 (borg profile, live-verified 2026-10-22).
- Manager session keying: internal/acp/manager.go GetOrCreate (sessions/
  pending maps keyed by agentID).
- Relay agent_id: internal/acp/relay.go:115 (PendingPermission.AgentID).
- Field pipeline pitfall: meept-design skill, integration-wiring-patterns.
