# Step CWD Session Fallback - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** resolveStepWorkingDir falls back to the linked session's CWD so `--cwd`-bound chats land files in the user's directory, never the daemon's.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Audit references:** finding F3 (file wrote into the meept repo), AGENTS.md daemon-CWD invariant

## Goal

2026-09-04: a step job told to "create hello.txt in the current directory"
resolved relative paths against the daemon CWD and wrote into
/Users/caimlas/git/meept/hello.txt. resolveStepWorkingDir
(internal/daemon/components.go:7089) tries WorktreePath then ProjectPath
from the task's linked sessions; a `meept chat --cwd /tmp/x` session has
neither — it has `CWD` (internal/session/session.go:56). This leaf adds the
CWD fallback so the session's working directory wins over the daemon's.

## Context

AgentJobProcessor.Process (components.go:7145) picks a task-scoped loop via
registry.GetForTask then calls resolveStepWorkingDir (7194). The session
store exposes Get(id) and GetByConversationID(id) — linked-sessions entries
are conversation IDs, hence the double lookup at 7097-7110.

Key files to understand before implementing:
- internal/daemon/components.go - resolveStepWorkingDir (7089-7137), Process cwd wiring (7188-7203)
- internal/session/session.go - Session struct: CWD (56), ProjectPath (76), WorktreePath (78); MemoryStore.Get / GetByConversationID

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/daemon/components.go — resolveStepWorkingDir lookup order:
//   1. linked session WorktreePath   (unchanged)
//   2. linked session ProjectPath    (unchanged)
//   3. NEW: linked session CWD       (first non-empty across sessions)
//   4. existing job.TaskID fallback block (unchanged, gains same CWD check)
// No signature change.
// Owner: 03. Consumers: 04, 06 (same file, later waves), 10 (e2e asserts).
```

### What This Leaf Consumes

```
// sessionStore.Get / GetByConversationID -> *session.Session with CWD field
```

## Tasks

### Task 1: CWD fallback in linked-session scan

**Objective:** When no WorktreePath/ProjectPath found, return the first non-empty linked-session CWD.

**Files:**
- Modify: `internal/daemon/components.go:7094-7122`
- Test: `internal/daemon/agent_job_processor_test.go` (create if absent;
  check existing components tests for the AgentJobProcessor setup pattern —
  search `AgentJobProcessor{` in _test.go files)

**Step 1: Write failing test**

```go
func TestResolveStepWorkingDir_CWDFallback(t *testing.T) {
	p := newTestAgentJobProcessor(t) // minimal processor with sessionStore + taskStore
	// Seed session: CWD="/tmp/user-session-dir", no ProjectPath/WorktreePath.
	// Seed task linked to that session (conversation ID form).
	job := &queue.Job{TaskID: "task-cwd-1"}
	got := p.resolveStepWorkingDir(job)
	if got != "/tmp/user-session-dir" {
		t.Errorf("resolveStepWorkingDir = %q, want session CWD", got)
	}
}
```

Also assert precedence: a session WITH ProjectPath still returns ProjectPath
(existing behavior), and no-sessions returns "".

**Step 2: Run test to verify failure**

Run: `go test -p 2 ./internal/daemon/ -run TestResolveStepWorkingDir -v`
Expected: FAIL - returns "" today (or daemon cwd).

**Step 3: Write minimal implementation**

Inside the linked-session loop, track `var cwdPath string`; on each session:

```go
if sess.CWD != "" && cwdPath == "" {
	cwdPath = sess.CWD
}
```

After the loop: `if projectPath != "" { return projectPath }`
then `if cwdPath != "" { return cwdPath }` before the job.TaskID fallback
block. Mirror the same CWD check inside the fallback block's session
lookups (7124-7137) so direct-session-key jobs also fall back.

**Step 4: Run test to verify pass**

Run: `go test -p 2 ./internal/daemon/ -run TestResolveStepWorkingDir -v`
Expected: PASS

### Task 2: precedence regression guard

**Objective:** Lock the full lookup order with a table test.

**Files:**
- Test: `internal/daemon/agent_job_processor_test.go`

**Step 1: Write test** — table: {worktree wins over project}, {project wins
over cwd}, {cwd used when others empty}, {empty when nothing set}. One
seeded session per case.

**Step 2: Run** `go test -p 2 ./internal/daemon/ -run TestResolveStepWorkingDir -v` — PASS.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] Lookup order documented in the function's doc comment

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] CWD fallback present in both the linked-session scan and the TaskID fallback block
- [ ] Precedence: WorktreePath > ProjectPath > CWD > ""
- [ ] Existing behavior for project-bound sessions byte-identical
- [ ] Tests cover all four precedence cases
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- AGENTS.md: "Never use os.Getwd() as a project/working-directory fallback
  in daemon code" — this fix uses the SESSION's recorded CWD, which is the
  sanctioned source; do not add any os.Getwd() call.
- The "Step job working dir resolved" log line at components.go:7196 will
  now also fire for CWD-sourced dirs — keep the log, it aids debugging.
- Leave 04 and 06 clear to edit components.go later: keep hunks tight.
