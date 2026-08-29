# Eval CLI - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** `meept eval run|show|list` writing RunRecord JSON under ~/.meept/eval/
- **Dependencies:** 01-eval-core.md
- **Estimated Context:** 50K
- **Concurrency Group:** B

## Goal

A user can run an oracle K times against a workdir and see Pass^k. This is how a human uses eval.

## Context

CLI pattern: `cmd/meept/<group>.go` with `runX(ctx, ..., io.Writer)` so tests skip cobra. Register on rootCmd in `cmd/meept/main.go`. Daemon CWD is irrelevant; `--workdir` is required.

Key files: `cmd/meept/main.go` (AddCommand site), sibling commands like `cmd/meept/agents_cmd.go` for cobra style.

## Interface Contracts (From Parent)

C1 RunRecord. Store path: `filepath.Join(home, ".meept", "eval", id+".json")`. Scratch is $HOME not /tmp.

### What This Leaf Exposes

```
meept eval run --task <id> --model <id> --k <n> --command <shell> --workdir <dir>
meept eval show <run-id>
meept eval list
# lowercase UI. exit 0 if Passed, 1 if not, 2 on usage error
```

### What This Leaf Consumes

C1 from leaf 01.

## Tasks

### Task 1: runEval CLI function

**Files:** Create `cmd/meept/eval.go`, `cmd/meept/eval_test.go`

`runEvalRun` executes ShellOracle k times in workdir, writes RunRecord, prints id + passed. Tests with a temp dir and `true`/`false`.

### Task 2: show + list

**Files:** Modify `cmd/meept/eval.go`

list newest-first, cap 50. show prints JSON. Missing id = error.

### Task 3: Register command

**Files:** Modify `cmd/meept/main.go` — only the AddCommand line for eval. If main.go is dirty with sibling hunks, report the exact line and do not sweep foreign hunks.

## Self-Verification Checklist

- [ ] Tests for run/show/list without cobra Execute
- [ ] Lowercase help text
- [ ] --workdir required
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] C1 JSON on disk
- [ ] No os.Getwd fallback
- [ ] main.go hunk is only eval registration
- [ ] Exit codes as specified
