# Trusted Root - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Applier deny-list for security/eval/oracles/self. Fix path traversal on backups.
- **Dependencies:** none
- **Estimated Context:** 45K
- **Concurrency Group:** A

## Goal

The thing that approves updates cannot be updated by those updates. Also stop `../.ssh` backup copies.

## Context

`internal/selfimprove/applier.go` Apply + backup. grok-findings.md noted traversal. Skills remain writable.

Key files: `internal/selfimprove/applier.go`, `applier_test.go`, validator.go.

## Interface Contracts (From Parent)

C5 ErrTrustedRoot and prefix list. Also deny absolute paths, `..` segments, and symlink escape from projectRoot.

CLI `meept agents set-gate` stays the only gate mutator. Applier must refuse patches that touch gate JSON5/config keys if they appear as file paths under config/.

### What This Leaf Exposes

`denyTrustedRoot`, traversal-safe backup path.

### What This Leaf Consumes

none.

## Tasks

### Task 1: Traversal tests RED then fix

**Files:** Modify `internal/selfimprove/applier.go`, `applier_test.go`

Cases: `../../.ssh/id_rsa`, absolute `/etc/passwd`, symlink outside root, `internal/security/engine.go`, `internal/eval/record.go`, `internal/selfimprove/applier.go`.

### Task 2: Allow a SKILL.md write

Control: a path under `config/skills/` or `.meept/skills/` still applies (existing approval rules).

### Task 3: Backup dest stays inside backupDir

Even if source was rejected we never copy out. If source accepted, backup join uses Base(name) only.

## Self-Verification Checklist

- [ ] -race internal/selfimprove
- [ ] ErrTrustedRoot wrapped with %w
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Deny list is in Go, not a skill
- [ ] No `//nolint` to skip the deny
- [ ] Skills still writable
