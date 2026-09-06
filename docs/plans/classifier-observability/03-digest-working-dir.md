# Digest Working Directory - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** SessionContextDigest gains the session's working directory (WorktreePath > ProjectPath > DetectionContext.CWD), and the analyzer's activity block includes it — resolving "current directory" references.
- **Dependencies:** none
- **Estimated Context:** 20K
- **Concurrency Group:** A

## Goal

"Create a file in the current directory" is genuinely ambiguous to an LLM
with no filesystem view. The session already knows its directory; the
digest should carry it. Precedence mirrors resolveStepWorkingDir
(WorktreePath > ProjectPath > CWD) so the analyzer sees the same
directory the step jobs will actually use.

## Context

buildSessionContextDigest (internal/agent/session_digest.go:42) already
receives sessionID; the dispatcher has d.sessionStore
(SessionStoreReader: Get/GetByConversationID → *session.Session with
WorktreePath/ProjectPath/DetectionContext.CWD fields —
internal/session/session.go:76-79 + DetectionContext at :55).
buildActivityBlock (internal/agent/intent_analyzer.go, added by the
session-aware-intent-gate tree) formats the block; it must append
"Working directory: <path>" when set.

Key files to understand before implementing:
- internal/agent/session_digest.go - digest struct + builder
- internal/agent/dispatcher.go:281-282, 492 - d.sessionStore (SessionStoreReader)
- internal/session/session.go:55-79 - Session fields; DetectionContext.CWD
- internal/agent/intent_analyzer.go - buildActivityBlock (search the const
  block + helper added by session-aware-intent-gate leaf 02)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/session_digest.go
//   SessionContextDigest gains: WorkingDirectory string
//   buildSessionContextDigest: after existing fields, resolve via
//     d.sessionStore.GetByConversationID(sessionID) (nil-guarded):
//     WorktreePath > ProjectPath > DetectionContext.CWD (nil-guard on
//     DetectionContext). First non-empty wins; unknown session → empty.
//   IsEmpty(): UNCHANGED — WorkingDirectory does NOT count as activity
//     (design decision: cwd is enrichment; an empty digest must stay
//     empty so contextless behavior is preserved).
// internal/agent/intent_analyzer.go
//   buildActivityBlock: append "\nWorking directory: <path>" when
//   d.WorkingDirectory != "".
// Owner: 03. Consumers: analyzer quality.
```

### What This Leaf Consumes

```
// d.sessionStore (SessionStoreReader — already a dispatcher field)
// session.Session fields WorktreePath/ProjectPath/DetectionContext
```

## Tasks

### Task 1: digest field + resolution

**Objective:** WorkingDirectory populated with correct precedence.

**Files:**
- Modify: `internal/agent/session_digest.go`
- Test: `internal/agent/session_digest_test.go`

**Step 1: Write failing tests** — table: (a) worktree wins over project;
(b) project wins over cwd; (c) cwd used when others empty (via
DetectionContext); (d) none set → empty; (e) nil sessionStore → empty,
no panic. Extend the existing digest table tests (they already seed
sessions via the daemon test pattern — reuse the seeding helper).

**Step 2: verify failure → implement (field + resolution in builder,
nil-guard d.sessionStore) → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run TestSessionContextDigest -count=1 -v`

### Task 2: activity block line

**Objective:** Analyzer block carries the directory.

**Files:**
- Modify: `internal/agent/intent_analyzer.go` (buildActivityBlock)
- Test: `internal/agent/intent_analyzer_test.go` (or the
  intent_session_rules_test.go file from the sibling tree — append, do
  not reorder)

**Step 1: Write failing test** — digest with WorkingDirectory set →
built user message contains "Working directory: /tmp/x"; empty → line
absent (no dangling newline).

**Step 2: verify failure → implement → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run 'ActivityBlock|SessionRules' -count=1 -v`

### Task 3: package green

`go test -p 2 ./internal/agent/ -count=1` + `go build ./...`.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] IsEmpty semantics unchanged (cwd not activity)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Precedence Worktree > Project > CWD, nil-guards everywhere
- [ ] IsEmpty ignores WorkingDirectory (documented)
- [ ] Activity block appends the line only when set
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- resolveStepWorkingDir's CWD source is DetectionContext.CWD — same
  source here; do NOT introduce os.Getwd anywhere.
- buildActivityBlock was added by session-aware-intent-gate leaf 02 —
  coordinate hunks; your change is append-only inside the helper.
