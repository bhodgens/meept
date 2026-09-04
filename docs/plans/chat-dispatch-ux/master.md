# chat-dispatch-ux - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 10 leaf documents under this node
- **Scope:** Make meept's chat-dispatch pipeline deliver honest, correct, user-shaped replies for naive users, per the 2026-09-04 meept-vs-hermes comparison findings.

## Goal

A 2026-09-04 roleplay test (naive user, "build a water reminder", model
agnes-2.5-flash, findings in `/tmp/meept-vs-hermes-findings.md`) exposed a
failure chain in meept's dispatch pipeline:

1. Sync-dispatch replies are the literal stub `Task <id> completed.` — the
   coder's real summary never reaches the user (`internal/agent/handler.go:1682`).
2. Steps whose execution errored are auto-approved and the task reports
   "completed" (`internal/agent/review_manager.go:153-188`).
3. Step-job loops resolve relative paths against the daemon CWD when the
   task's session has no project/worktree binding — a test write landed in
   the meept repo itself (`internal/daemon/components.go:7089` never falls
   back to `sess.CWD`, `internal/session/session.go:56`).
4. Task-scoped agent loops never receive the tool-schema-mode config, so the
   coder loop carries the full 75-tool catalog and burns 49k/50k context on
   trivial tasks (only `c.AgentLoop` gets `SetSchemaModeConfig`,
   `internal/daemon/components.go:1422`).
5. Raw `platform_status`/`platform_tools`/`platform_agents` JSON output can
   become the final chat reply (observed: session msg 792 was a status JSON
   dump; another run returned a 28-agent roster with system-prompt bodies).
6. A terminal quota error inside a specialist loop surfaces to the user as
   "my tools aren't working" — the harness never states the real cause.
7. Skill discovery matched `go-linter-save-hook-revert` (0.72) to a
   water-reminder request — generic verb/noun overlap drives false positives.
8. Claude-skill imports spam 12k+ warnings per day: the parser rejects
   comma-separated `allowed-tools:` scalars and repeats every warn on every
   scan (`internal/skills/models.go:146`, `internal/skills/source_claude.go:115`).
9. `agent.result` is a known orphan publisher with no annotation in the
   connectivity graph (`scripts/gen-connectivity-graph.py:701`).

This tree fixes all nine. Hermes comparison bar: every turn ends with what
was done + how to run it; failures state the harness cause; artifacts land
in the directory the user started from.

## Architecture

All fixes stay inside the existing dispatch pipeline: ChatHandler sync-wait
returns real step results; ReviewManager stops laundering errors into
approvals; AgentJobProcessor resolves cwd from the linked session; the
registry passes schema-mode config into lazily-created task-scoped loops; a
reply guard filters raw platform-catalog output in AgentLoop.RunOnce; job
execution publishes quota events on the existing `agent.quota_wait` topic;
skill discovery gains a domain-agreement gate; the skill parser tolerates
comma-separated scalars and dedupes warnings; the graph script annotates the
known orphan. No new bus topics, no new config surfaces beyond extending
existing ones, no API changes.

## Interface Contracts

### Contract 1: Sync reply carries real result (C1)

```
// internal/agent/handler.go — waitForTaskCompletion
// On terminal state: fetch terminal steps via h.stepStore.ListByTaskID,
// pick the last completed/approved step's Result (fallback: any non-empty
// Result). Reply = that result when non-empty; the current
// "Task %s completed." string ONLY as last-resort fallback when every
// step Result is empty. Failed tasks return the failure text from
// handleTaskFailed's shape, never "completed".
// Owner: 01. Consumers: chat RPC path (proxy.go "chat"), 10.
```

### Contract 2: Honest completion payloads (C2)

```
// internal/agent/review_manager.go — error steps return ReviewRejected
// (or a new ReviewFailed outcome) and NEVER ReviewApproved.
// internal/agent/tactical.go — a task with any failed step completes as
// StateFailed (or StatePartial if such state exists) with the error text
// in the task.completed payload "result" field; step Result truncation in
// buildStepSummaries raised 100 -> 400 chars for the reply path.
// Owner: 02. Consumers: 01, 10.
```

### Contract 3: Step cwd session fallback (C3)

```
// internal/daemon/components.go — resolveStepWorkingDir
// After WorktreePath and ProjectPath checks fail, fall back to the linked
// session's CWD field (session.go:56). Lookup order unchanged otherwise.
// Owner: 03. Consumers: 04 (same file, later wave), 10.
```

### Contract 4: Task-scoped loops inherit schema mode (C4)

```
// internal/agent/registry.go — AgentRegistry gains SetSchemaModeConfig(cfg
// config.AgentToolsConfig) storing cfg; createLoop applies the same
// resolution as AgentLoop.applySchemaModeLocked (indexed default) to each
// new task-scoped loop's registry via SetSchemaMode.
// internal/daemon/components.go — daemon wires registry.SetSchemaModeConfig
// alongside the existing c.AgentLoop.SetSchemaModeConfig (components.go:1422).
// platform_tools/platform_agents/tool_view stay in DefaultAlwaysFullTools().
// Owner: 04. Consumers: 10.
```

### Contract 5: No raw catalog replies (C5)

```
// internal/agent/loop.go — RunOnce (or its response-assembly site) inspects
// the final assistant text: if it is (or is dominated by) unshaped JSON /
// platform_* tool output dumps (heuristic: starts with '{' AND contains
// platform_status|platform_tools|platform_agents payload keys, or matches
// the "## Available Agents" roster header), replace with a short
// user-language summary naming the tool called and advising re-ask.
// Heuristic lives in one new function with unit tests.
// Owner: 05. Consumers: 10.
```

### Contract 6: Quota failures surface to the user (C6)

```
// internal/daemon/components.go — AgentJobProcessor.Process error path:
// errors.As *llm.QuotaResetError -> publish agent.quota_wait bus event
// (existing topic; WS classifies as agent_progress — AGENTS.md invariant)
// with keys: conversation/session (from step payload or task linked
// sessions), class="quota_wait", unblock_at, agent_id, task_id. Job still
// fails; the user sees a quota-state message instead of nothing.
// Owner: 06. Consumers: 10.
```

### Contract 7: Skill discovery domain gate (C7)

```
// internal/agent — discoverRelevantSkills path gains a domain-agreement
// check: a match only surfaces when >=1 of the query's domain tokens
// (non-stopword, length>=4) appears in the skill's name/tag/description.
// Extends the stopword work from commit e0d08e2f; threshold default 0.5
// unchanged.
// Owner: 07. Consumers: 10.
```

### Contract 8: Skill parser list-field tolerance (C8)

```
// internal/skills/models.go — AllowedTools (and Triggers/Tags/Requires)
// gain scalar tolerance: a comma-separated string unmarshals into []string
// (custom UnmarshalYAML or a pre-split normalization in parseMetadata).
// internal/skills/source_claude.go — parse-failure warnings dedupe: first
// occurrence per path logs WARN, repeats log Debug.
// Owner: 08. Consumers: none.
```

### Contract 9: Orphan annotation (C9)

```
// scripts/gen-connectivity-graph.py:701 — annotated_orphans gains
// "agent.result": reason string (agent-loop result publish; subscriber is
// the external/TUI path). Regenerate docs/generated via make graphs.
// Owner: 09. Consumers: make graphs-check CI.
```

### Contract 10: E2E regression transcript (C10)

```
// scripts/e2e-naive-user-chat.sh — builds bin/, starts a scratch daemon on
// a temp state dir + socket, runs the naive-user transcript (build ->
// modify -> status -> file-location check) over RPC with --cwd pointing at
// a tmpdir, asserts: non-stub replies (no "Task .* completed\.$"), no
// "## Available Agents" / raw JSON dumps, created file exists under the
// tmpdir. Skippable via short mode. Owner: 10.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-sync-reply-result.md | leaf | none | 30K | A |
| 02 | 02-honest-completion.md | leaf | none | 30K | A |
| 03 | 03-step-cwd-fallback.md | leaf | none | 25K | A |
| 05 | 05-catalog-reply-guard.md | leaf | none | 25K | A |
| 08 | 08-skill-parser-tolerance.md | leaf | none | 25K | A |
| 09 | 09-graph-orphan-annotation.md | leaf | none | 10K | A |
| 04 | 04-task-loop-schema-wiring.md | leaf | 03 (same file) | 30K | B |
| 06 | 06-quota-user-surfacing.md | leaf | 03 (same file) | 30K | B |
| 07 | 07-skill-discovery-gate.md | leaf | 05 (same file) | 25K | B |
| 10 | 10-e2e-regression.md | leaf | 01,02,03,04,05,06 | 35K | C |

**Concurrency groups:** A leaves touch disjoint files and dispatch
simultaneously. B leaves each share a file with an A leaf — dispatch only
after that A leaf is committed. C runs last against the integrated daemon.

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group A

Dispatch 01, 02, 03, 05, 08, 09 simultaneously via `delegate_task`:

- Goal: "Implement all tasks from <leaf path>"
- Context: full leaf text + relevant contracts from this master + AGENTS.md
  conventions block + the anchor snippets cited in the leaf (INLINED)
- Include: "Do NOT commit. Do NOT run git add. Write code, run scoped tests,
  report results only."
- Include: "Do NOT use read_file on existing source files — explore with
  search_files or terminal cat. After writing a file, do NOT read it back."
- Include: "macOS: run scoped package tests with -p 2; never unbounded
  go test ./..."

### Phase 2: Review and Commit Each Child

Orchestrator reviews in-session (NOT via delegated reviewer):

1. Read the changed files; check against leaf spec + contracts + Review
   Checklist below. Run the leaf's scoped tests plus `make mutexio predid`
   when handler/tactical/components changed.
2. Gaps: re-dispatch with specific feedback (max 3 cycles, then escalate).
3. Pass: commit exact paths — `git add <paths> && git commit -m
   "fix(chat-dispatch-ux): <leaf scope>"` — update tracking table to
   REVIEWED. Sibling sessions commit mid-session: re-check `git log` and
   `git status` before every commit; stage only the leaf's paths.

### Phase 3: Dispatch Group B, then Group C

- After 03 commits, dispatch 04 and 06 (both edit components.go — run them
  SEQUENTIALLY, 04 then 06, to avoid hunk collisions).
- After 05 commits, dispatch 07.
- After all of 01-09 reach REVIEWED, dispatch 10 (e2e regression), then run
  the Integration Test Plan, normalize with gofmt, verify no line-number
  corruption (`grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/ cmd/`
  returns zero), update AGENTS.md + docs/workflows, commit integration.

## Review Checklist

The orchestrator verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print debugging, no TODOs, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes in source files
- [ ] AGENTS.md touched if the leaf invalidates any statement in it

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+ (leaves 01-07, 10), Python 3 (leaf 09)
- **Naming:** exported PascalCase, unexported camelCase; no stuttering
- **Errors:** wrap with %w, two-value type assertions on map[string]any
- **No ignored errors** (`_ = f()` is blocked by pre-commit); no bare panic
- **Mutex scope:** never hold a mutex across I/O; collect-then-operate
- **IDs:** pkg/id.Generate only — never time.Now().UnixNano/math/rand
- **Bus payloads:** new keys documented; reuse agent.quota_wait for quota
  events — never a new topic prefix (AGENTS.md invariant)
- **UI text:** lowercase in any user-facing strings
- **Testing:** table-driven where natural; scoped `go test -p 2 ./internal/<pkg>/ -run X`
- **Formatting:** gofmt before reporting; Python leaf mirrors existing script style

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-sync-reply-result | COMPLETE | 1 | commit b3eb4878; no deviations; stub-guard test pins fallback semantics |
| 02-honest-completion | COMPLETE | 1 | commit 892efbe3; gate hoisted above policy paths (required, red-phase verified); firstLine reused; success guard test added |
| 03-step-cwd-fallback | COMPLETE | 1 | commit 39884594; deviation: CWD lives in Session.DetectionContext.CWD (nil-guarded), not a top-level field — implementer correct, master anchor imprecise |
| 04-task-loop-schema-wiring | COMPLETE | 1 | commit 845f9efa; parent-registry application (FilteredToolRegistry delegates — covers cached loops); wiring at :2277 (registry nil at :1422) |
| 05-catalog-reply-guard | COMPLETE | 1 | commit aca55f78; line-shape prose ratio (documented deviation); carries 2 sibling-audit loop.go hunks (H1/C2) — flagged in commit msg for that session |
| 06-quota-user-surfacing | COMPLETE | 1 | commit 93a11911; WithBus setter added (none existed); Contract-6 sentence over Task-1 snippet; Task 2 needed zero impl (verified data flow); 1 transient suite FAIL was flake — two clean reruns |
| 07-skill-discovery-gate | COMPLETE | 1 | commit c3b334cd; StopWordSet accessor (single source); carries 2 sibling-bughunt loop.go hunks (D-C3/D-H2) flagged in msg |
| 08-skill-parser-tolerance | COMPLETE | 1 | commit b5c0fd1e; stringList mechanism; alt-name passes swapped too (blessed by leaf Notes); full repo build green at commit |
| 09-graph-orphan-annotation | COMPLETE | 1 | commit 6ad221a0; graphs-check green; regen rerun scheduled at integration gate (working-tree drift from leaves 02/04 + sibling audit) |
| 10-e2e-regression | COMPLETE | 2 | commit 95aa327a; final live run 16 PASS/1 FAIL (A5 continuity thin spot — follow-up leaf candidate)/0 SKIP |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build -o bin/meept-daemon ./cmd/meept-daemon && go build -o bin/meept ./cmd/meept`
2. `go test -p 2 ./internal/agent/ ./internal/daemon/ ./internal/skills/ ./internal/task/`
3. `make graphs && make graphs-check`
4. `make lint-ci` (golangci-lint + analyzers + audit scripts)
5. `bash scripts/e2e-naive-user-chat.sh` (leaf 10) — asserts the full
   transcript: real replies, correct cwd placement, no catalog dumps.
6. Manual spot-check mirroring the original finding: `./bin/meept chat
   --cwd /tmp/water-reminder-meept2 "create hello.txt in the current
   directory"` → file exists in /tmp/water-reminder-meept2, reply names it.
7. Cross-contract checks: failed-step task returns failure text through the
   chat RPC (C1+C2); quota'd provider yields a user-visible quota message
   (C1+C6); coder loop context stays under ~20k on a trivial task (C4).

## Open Questions

- None blocking. If leaf 04 discovers createLoop cannot reach a registry
  per-loop (registries are shared), the fallback is applying schema mode in
  the GetForTask branch of components.go:7179-7203 right after loop
  creation — same contract, alternate site. Record the choice in the leaf's
  Deviations.

## Structural Completeness Check (Before Dispatch)

Required sections present in this master: Dispatch Protocol, Interface
Contracts, Child Index, Review Checklist, Coding Conventions, Completion
Tracking Table, Integration Test Plan. Run:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep chat-dispatch-ux
```

Must print ALL TREES COMPLIANT: True before any dispatch.

## Notes

- Findings source: /tmp/meept-vs-hermes-findings.md (2026-09-04 session).
  Line anchors in leaves were verified against HEAD 2026-09-04; sibling
  sessions commit mid-session — re-verify before dispatch (see
  hierarchical-planning references/execution-readiness-audits.md).
- The daemon under test is the USER'S RUNNING DAEMON (pid 8910). Do not
  restart or kill it from leaves. Leaf 10's script starts its OWN daemon on
  a temp state dir/socket/port.
- Truncated task names in completion logs ("create a file named hello.")
  come from dispatcher task naming — cosmetic, out of scope here.
