# Session-Aware Intent Gate - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 3 leaf documents under this node
- **Scope:** Give the dispatcher's intent-analyzer session context so follow-up turns ("did the change get made? where is the file?") are judged with history, not in isolation — fixing the A5 canned-clarification gap.

## Goal

The chat-dispatch-ux tree fixed reply honesty; the session-continuity tree
fixed history recording/restore. The e2e still fails A5 because of a THIRD,
pre-existing gap: the dispatcher's intent-analyzer ambiguity gate
(internal/agent/dispatcher.go:665-668) judges each message IN ISOLATION.
`AnalyzeTrueIntent` (internal/agent/intent_analyzer.go:106) sends only
{system prompt, raw input} to its LLM — structurally blind to the session.
A status-shaped follow-up ("did the change get made? where is the file?")
scores ambiguity >= 0.6 → `buildClarificationResult` returns a canned
clarification BEFORE any agent (with its restored history) is invoked.

Architectural principle (user-stated): agents that address the user query
session context at turn time; the session does not passively absorb agent
context. The analyzer is a user-addressed agent (its questions go to the
user), so it queries session state. The dispatcher already holds both
handles: `TaskStore` (dispatcher.go:299, `GetTasksForSession` at
internal/task/store.go:423) and `TaskRegistry` (:300 → `StepStore()`),
plus `sessionTracker.GetLastIntent`.

## Architecture

A new `SessionContextDigest` (compact string + metadata) is built by the
dispatcher per turn from existing stores — last task for the session
(name/state/agent), its terminal step result summary, and last intent —
and passed to the analyzer as an additional block in the user message.
The analyzer's prompt gains rules for context-conditioned ambiguity.
Both existing call sites (dispatcher.go:665 fresh input, :1266
post-clarification combined input) pass the digest. Empty session →
empty digest → byte-identical behavior today. Analyzer LLM failure →
already fails open. No thresholds, no keyword lists, no new config.

## Interface Contracts

### Contract 1: SessionContextDigest builder (SG1)

```
// internal/agent/session_digest.go (new file, package agent)
// type SessionContextDigest struct {
//     LastTaskName    string // e.g. "create a file named hello.txt..."
//     LastTaskState   string // "completed" | "failed" | "" (none)
//     LastTaskAgent   string // e.g. "coder"
//     LastResultSummary string // terminal step Result, first line, cap 400
//     LastIntentType  string // from SessionTracker.GetLastIntent
// }
// func (d *Dispatcher) buildSessionContextDigest(sessionID string)
//   *SessionContextDigest
//   - taskStore.GetTasksForSession(sessionID), take tasks[0] (most
//     recently updated), fetch its steps via
//     d.taskRegistry.StepStore().ListByTaskID(task.ID), take the
//     highest-sequence completed/approved step's Result
//   - lastIntentType from d.sessionTracker.GetLastIntent(sessionID)
//   - ALL store access nil-guarded; any error → zero-value digest
//   - digest.IsEmpty() bool helper
// Owner: 01. Consumers: 02 (analyzer), 03 (e2e).
```

### Contract 2: Context-conditioned analysis (SG2)

```
// internal/agent/intent_analyzer.go
// func (ia *IntentAnalyzer) AnalyzeTrueIntent(ctx context.Context,
//   input string, sessionContext *SessionContextDigest)
//   (*TrueIntentAnalysis, error)
//   - signature EXTENDED (both dispatcher call sites updated: :665 and
//     :1266); nil sessionContext or IsEmpty() → prompt identical to
//     today's (byte-for-byte behavior preserved for contextless turns)
//   - non-empty digest appends to the USER message (after the input):
//     "\n\n[Recent session activity]\nLast task: <name> (state: <state>,
//     agent: <agent>)\nResult summary: <summary>\n" (fields omitted when
//     empty), plus system-prompt rules:
//       - "Recent session activity is provided when available. Use it to
//         resolve pronouns and references ('the change', 'the file',
//         'it') against what was just done."
//       - "If the input is a short follow-up question about the recent
//         activity and the activity makes the referent clear, ambiguity
//         is LOW (proceed) — do not ask the user to re-specify."
//   - parseAnalysis unchanged
// Owner: 02. Consumers: 03 (e2e A5 goes green).
```

### Contract 3: E2E A5 flips green deterministically (SG3)

```
// scripts/e2e-naive-user-chat.sh — A5 assertion text unchanged (leaf 03
// of session-continuity already tightened it); this leaf RUNS the suite
// and A5 must PASS on two consecutive provider-available runs, with the
// T3 reply referencing hello.txt and no bare-clarification signature.
// Owner: 03. Consumers: integration gate.
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-session-context-digest.md | leaf | none | 30K | A |
| 02 | 02-analyzer-session-rules.md | leaf | 01 (digest type + wiring) | 30K | B |
| 03 | 03-e2e-a5-verify.md | leaf | 01, 02 | 20K | C |

## Dispatch Protocol

### Phase 1: Dispatch Concurrency Group A

Dispatch 01 via `delegate_task` (full leaf text + contracts + conventions
+ anchor snippets inlined; no-commit; no read_file; TDD; scoped -p 2
tests). Single leaf — 02 depends on the digest type 01 defines.

### Phase 2: Review and Commit

Orchestrator reviews in-session against leaf spec + contracts; commits
exact paths after scoped tests pass. Shared-tree hazards: check
`git log`/`git status` before every commit; hooks bypass requires manual
package verification + commit-message note; stage ONLY the leaf's paths.

### Phase 3: Groups B then C

- After 01 commits, dispatch 02 (edits intent_analyzer.go + both
  dispatcher call sites).
- After 02 commits, dispatch 03 (e2e runs; A5 must pass twice).
- Integration: `go build ./...`, `go test -p 2 ./internal/agent/ -count=1`,
  full e2e, AGENTS.md invariant update, completion report.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD)
- [ ] Code follows project conventions
- [ ] No scope creep
- [ ] No debug artifacts, no line-number corruption
- [ ] AGENTS.md touched if the leaf invalidates a statement in it

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go 1.22+; wrap errors with %w; two-value map assertions
- **No ignored errors; no panic; mutex never held across I/O**
- **Testing:** stdlib testing, table-driven where natural, scoped
  `go test -p 2 ./internal/agent/ -run X -count=1 -v`
- **Never commit; never git add; do NOT modify .md plan files**
- **Do NOT use read_file on existing sources — search_files/terminal**
- **After writing a file do NOT read it back**
- **macOS: never unbounded go test ./... (ephemeral-port exhaustion)**

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-session-context-digest | COMPLETE | 1 | commit f1b50205; reuses bestStepResult/firstLine/truncateString; no call-site changes |
| 02-analyzer-session-rules | COMPLETE | 1 | commit 6bcb53c2; wire-level byte-identity proof (httptest); both call sites wired; analyzer seam integration test |
| 03-e2e-a5-verify | IN_PROGRESS | 0 | 15 runs. Run 14: window closed mid-run again (11 quota hits, 0 tasks). Provenance Meta from classifier-observability leaf 01 VISIBLE in captured reply (meta.session_digest_used=true) — cross-tree payoff confirmed live. Analyzer verdicts hovering AT 0.6 threshold on fallback models. STATUS: 15-run acceptance record is now dominated by provider instability; per stop-clause (c), awaiting user decision on: (a) keep cycling, (b) swap provider, or (c) close COMPLETE-with-caveat |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...`
2. `go test -p 2 ./internal/agent/ -count=1` (full package)
3. `bash -n scripts/e2e-naive-user-chat.sh`
4. `bash scripts/e2e-naive-user-chat.sh` TWICE — both runs must show
   A5 PASS (plus A1-A4/A6 unchanged); provider-unavailable → honest SKIP
   and re-run.
5. `make analyzers` (mutexio/predid/selflock) on touched packages.

## Open Questions

- None. Digest caps: task name cap 200 chars, result summary cap 400
  chars, single task (most recent) — decided at authoring time to keep
  the analyzer prompt small.

## Structural Completeness Check (Before Dispatch)

Run:
```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep session-aware-intent-gate
```
Tree OK + all leaves OK required before any dispatch.

## Notes

- A5 history: the one FAIL in chat-dispatch-ux leaf 10 (16 PASS/1 FAIL);
  root cause isolated in session-continuity leaf 03 (commit 9bd2c6a4).
  Leaves 01/02 of session-continuity (history recording + restore) are
  prerequisites and are already landed — this tree completes the chain
  by making the GATE context-aware.
- dispatcher.go and intent_analyzer.go may carry sibling WIP: `git diff`
  before editing, keep hunks minimal.
- Do NOT change ambiguity threshold (0.6), do NOT add keyword lists, do
  NOT add config. The model decides from context.
