# Quality-Gated Autonomy - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Goal completion requires passing a configurable shell gate; unchanged workspaces skip re-running failed gates.
- **Deps:** none (employee package exists standalone) | **Context:** 55K | **Group:** B

## Goal

GoalLoop REFLECT can mark goals complete on model judgment alone. prime-agent semantics: an autonomous run may only claim success after a user-defined check passes (`--autonomous-gate "npm test"`), and a failed gate is not re-run when the workspace hash is unchanged. Wire the same discipline into employee goals.

## Context

internal/employee/goal_loop.go — ASSESS->PLAN->EXECUTE->REFLECT cycle; goal health states; enforcement engine separate (do not conflate — this is completion gating, not action policing). ExecutionBackend available for shell runs (runtime pkg). Workspace = employee's project dir via existing wiring.

Key files: internal/employee/goal_loop.go (+_test), goal.go, config schema [employee] block, docs/workflows/employees.md.

## Interface Contracts (From Parent)

```go
// internal/employee/gate.go (new):
type GateConfig struct {
    Command          string `json:"command"`            // empty = no gate (legacy)
    TimeoutSeconds   int    `json:"timeout_seconds"`    // default 300
    SkipWhenUnchanged bool  `json:"skip_when_unchanged"`// default true
}

type GateResult struct{ Passed bool; Output string; Skipped bool; Reason string }

func RunGate(ctx context.Context, cfg GateConfig, backend runtime.ExecutionBackend, workdir string, prev *GateState) (*GateResult, GateState, error)
// GateState{WorkspaceHash string; LastFailedOutput string}
// workspaceHash = sha256 over `git status --porcelain` output + HEAD sha (cheap determinism).
// SkipWhenUnchanged && prev.WorkspaceHash==current && !prev.LastFailedOutput=="" -> skipped result.
```

GoalLoop REFLECT integration:
- Goal gains Gate GateConfig (per-goal JSON5 in employee definition; defaults from [employee.defaults.gate]).
- Completion path: gate.Command != "" -> MUST RunGate pass before status completed; fail -> health stays yellow/red per existing mapping, failed OUTPUT appended to next PLAN prompt as feedback block (truncated 4KB).
- Gate execution uses configured backend honoring require_sandbox from runtime config (containment tree integration point — read runtime.Config if present; else local).
- Audit: gate runs recorded in existing employee audit trail.

## Tasks
1. Failing tests gate.go: pass/fail/skip matrix incl. timeout kill; hash stability across no-op git state; skip logic only when previous failure + unchanged.
2. Failing loop tests (existing harness patterns): scripted REFLECT w/ failing gate -> cannot complete, feedback present next round; passing gate -> completes; no gate -> legacy behavior.
3. Config plumbing + employee JSON5 schema doc line + employees.md section.
4. CLI surface: extend `meept agents show <id>` output with gate status line (lowercase).

## Self-Verification Checklist
- [ ] -race green internal/employee
- [ ] Legacy goals (no gate) byte-behavior identical
- [ ] Gate output never logged above debug (may contain code)

## Review Checklist
- [ ] Backend injection nil-safe (falls to local exec w/ warn when runtime disabled)
- [ ] Hash uses existing git helpers where present (no duplicate procs)
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: do NOT touch enforcement engine checkpoints — orthogonal layer.
