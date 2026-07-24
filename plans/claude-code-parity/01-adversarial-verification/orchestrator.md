# Tree 01: Independent Adversarial Verification

## Goal

Add an independent adversarial verification system that auto-spawns after non-trivial changes, fuses with the existing EvidenceRequirements contract, and produces structured PASS/FAIL/PARTIAL verdicts with command-output evidence.

## Architecture Overview

Three layers:
1. **Verification mode** on agent definitions (alternate front matter) — configures per-agent verification behavior (enabled, model override, auto-trigger, max fix loops)
2. **Adversarial verifier prompt** — borrowed from Claude Code's verification agent, adapted for meept's tool ecosystem. Fused with EvidenceRequirements: the verifier demands the same evidence types (file_exists, file_hash, process_exit, shell_output) but adversarially
3. **Auto-trigger + fix loop** — integrated into the agent loop's post-turn logic. Triggers after N file edits (configurable). On FAIL: re-dispatches implementer with verifier findings, loops up to max_fix_loops, then escalates to user

## Interface Contracts

See SHARED-CONVENTIONS.md §6 for the verification mode JSON5 schema and daemon config schema.

### Verifier Output Format (parsed by auto-trigger logic)
```
### Check: [what is being verified]
**Command run:**
  [exact command]
**Output observed:**
  [actual output, copy-paste]
**Result: PASS** (or FAIL with Expected vs Actual)

VERDICT: PASS | FAIL | PARTIAL
```

The auto-trigger logic parses the `VERDICT:` line. PARTIAL is treated as PASS with a warning logged.

### Verifier Tool Restrictions
The verifier agent runs with a restricted tool set:
- ALLOWED: shell (read-only commands only — enforced by security engine's readOnlyCommands allowlist), file_read, file_grep, file_find, list_directory, web_fetch
- BLOCKED: file_write, file_edit, file_delete, git_commit, memory_store, task_create, task_update, ask, resolve

## Child Index

| # | Leaf | Est. Context | Dependencies | Files Touched |
|---|------|-------------|--------------|---------------|
| 01 | verification-mode | 80K | none | ~12 files |
| 02 | adversarial-prompt | 90K | 01 (mode types) | ~8 files |
| 03 | auto-trigger-loop | 85K | 01, 02 | ~10 files |

## Dispatch Protocol

1. Dispatch leaf 01 first (defines types and agent spec extensions).
2. After 01 reviews, dispatch 02 and 03 concurrently (both depend on 01's types but not each other).
3. Review each in-session. Commit after review.
4. Integration: verify verification mode appears in all built-in agent definitions.

## Review Checklist

- [ ] All built-in agents in `config/agents/` have `verification` front matter
- [ ] Verification mode respects `enabled: false` (agent skips verification)
- [ ] Model override works (verifier uses configured model, not agent's model)
- [ ] Adversarial prompt includes anti-rationalization section
- [ ] Verifier cannot write files (tool restriction enforced)
- [ ] VERDICT parser handles PASS, FAIL, PARTIAL, and malformed output
- [ ] Auto-trigger fires after N file edits (configurable threshold)
- [ ] Fix loop respects max_fix_loops from daemon config
- [ ] Fix loop escalation to user works after max iterations
- [ ] EvidenceRequirements fusion: verifier demands same evidence types as baseline
- [ ] No debug artifacts, no TODOs, no placeholder values

## Coding Conventions

See SHARED-CONVENTIONS.md §2-§3.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-verification-mode | COMPLETE | VerificationConfig, all 18 AGENT.md updated |
| 02-adversarial-prompt | COMPLETE | BuildVerifierPrompt, ParseVerdict, verifier agent |
| 03-auto-trigger-loop | COMPLETE | VerificationTracker, VerificationAutoTrigger hook |

## Integration Test Plan

1. `go build ./...`
2. `go test ./internal/agent/... -race -run TestVerification`
3. Verify all `config/agents/*.json5` files parse with new `verification` field
4. Verify verifier prompt contains "your job is not to confirm" and "VERDICT:"
5. Verify tool restriction blocks file_write for verifier role
