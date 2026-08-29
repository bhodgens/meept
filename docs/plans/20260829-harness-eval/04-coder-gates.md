# Coder Gates - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Roster coding agents get gate.command; mutating turns must pass the gate. Global kill switch stays false.
- **Dependencies:** 01-eval-core.md
- **Estimated Context:** 60K
- **Concurrency Group:** B

## Goal

Coder and debugger cannot report success after writing code unless `go test ./...` exits 0. Interactive chat without file mutations stays ungated. Employees already have Goal.Gate — do not rebuild it.

## Context

`internal/employee/gate.go` RunGate exists. Reuse it. AGENT.md parser: `internal/agents/models.go` AgentMetadata. Conversion: `internal/agent/spec.go` / registry. Loop or executor end-of-turn is the run site — prefer executor/spec hook, not a 200-line loop.go rewrite. If you must touch loop.go, only one call site and this leaf then BLOCKS 05/06/11 until merged.

Key files: `config/agents/coder/AGENT.md`, `config/agents/debugger/AGENT.md`, `internal/agents/models.go`, `internal/agents/parser.go`, `internal/employee/gate.go`.

## Interface Contracts (From Parent)

C2 GateMetadata. Mutating tools: file_write, file_edit, file_delete, shell_execute (non-read). Read-only skip.

`employees.defaults.gate.enabled` remains false. Roster gate is per-agent command, not the employee kill switch. When the kill switch is false, employee goals stay ungated; roster AGENT.md gate still runs (different path).

### What This Leaf Exposes

AGENT.md `gate.command`. Spec field on AgentSpec. End-of-turn RunGate in the session workdir (not daemon CWD).

### What This Leaf Consumes

employee.RunGate. Session working dir from tools context / LinkedSessions (conv vs session id — try Get then GetByConversationID).

## Tasks

### Task 1: Parse gate frontmatter

**Files:** Modify `internal/agents/models.go`, parser tests in `internal/agents/`

Roundtrip YAML. Empty command = no gate.

### Task 2: Bundled AGENT.md

**Files:** Modify `config/agents/coder/AGENT.md`, `config/agents/debugger/AGENT.md`

Add:
```
gate:
  command: "go test ./..."
  timeout_seconds: 300
  skip_when_unchanged: true
```
Keep verification block. Lowercase body text already.

### Task 3: Run gate after mutating turn

**Files:** New `internal/agent/roster_gate.go` + tests. One call from the existing post-turn path (grep where verification.auto_trigger runs; hang next to it).

Failing gate: turn result includes gate output (4KB cap); do not mark task completed. Passing: no extra user-visible banner (lowercase log only).

### Task 4: Docs line

**Files:** `docs/workflows/employees.md` or `docs/workflows/agents.md` — one short subsection: roster gate vs employee gate.

## Self-Verification Checklist

- [ ] Parser tests
- [ ] coder+debugger AGENT.md updated
- [ ] RunGate reused, not copied
- [ ] workdir not os.Getwd
- [ ] Global kill switch default still false
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Reviewer agents not gated
- [ ] Read-only coder turn skips gate
- [ ] session_id vs conversation_id cwd resolution
