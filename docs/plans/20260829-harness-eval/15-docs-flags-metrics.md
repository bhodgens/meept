# Docs, Flags, Metrics - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Honest docs, two-layer feature flags for eval iteration, coverage/Pass^k metrics. After leaves 01-14.
- **Dependencies:** 01 through 14
- **Estimated Context:** 45K
- **Concurrency Group:** F

## Goal

features.md must not claim closed-loop shadow training. Eval flags let us ablate harness pieces. Metrics record Pass^k and trajectory coverage.

## Context

`docs/features.md` shadow section (~1312). `docs/implementation-gaps.md` is dated 2026-02-21 — update or point to this tree. AGENTS.md package table: add `internal/eval` if missing.

Two-layer flags (book ch7): a runtime flag to disable a harness piece (gate, status bar, isolation) without a rebuild; config-layer defaults in schema.go.

Key files: `docs/features.md`, `docs/workflows/`, `internal/config/schema.go`, `internal/metrics/`.

## Interface Contracts (From Parent)

Config:

```
[eval]
enabled = false
# ablation knobs (default on when eval.enabled):
gates = true
status_bar = true
isolation = true
```

When eval.enabled=false, production behavior is unchanged (gates still follow leaf 04 AGENT.md). Flags are for eval runs/model-swap, not a user-facing off switch for security.

Metrics: counter `eval_pass_k_total{task,passed}` and `eval_oracle_runs_total`. Subscribe in-process (not TUI-only) so daemon-only runs record.

### What This Leaf Exposes

Docs + config + metrics observer.

### What This Leaf Consumes

C1 runs, C8 status, C4 isolation.

## Tasks

### Task 1: Docs honesty

**Files:** `docs/features.md` shadow/training claims; `docs/implementation-gaps.md` status; AGENTS.md Key Components table for eval. Feature mapping `docs/workflows/eval.md` new short file.

### Task 2: [eval] config

**Files:** `internal/config/schema.go` + DefaultConfig + Validate unknown keys fail-fast. Do not fight siblings: re-read schema.go before patch.

### Task 3: Metrics observer

**Files:** `internal/metrics` subscribe to eval completion. Test with a fake bus publish.

## Self-Verification Checklist

- [ ] No “closed-loop LoRA” claim remains in features.md
- [ ] AGENTS.md lists internal/eval
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Security isolation flag cannot disable fence/tirith (only spawn isolation)
- [ ] metrics have an in-process subscriber
