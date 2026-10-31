# 00T: Branch Test Gate for 03-membership - Test-Only Leaf

> **For the implementing agent:** Write TESTS ONLY. No implementation.
> RED for missing-implementation reasons. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Tests for membership fields, join, and the mutation gate.
- **Dependencies:** none (tests reference symbols that do not exist yet)
- **Estimated Context:** 45K
- **Concurrency Group:** T

## Goal

Prove the branch's membership features RED before any of them lands: fields
parse and merge correctly, the join is deterministic, and the mutation gate
denies everyone but the agent-update agent and admin humans - at BOTH the RPC
seam and the file-tool backstop.

## Tasks

| Task ID | Objective | Files | RED reason |
|---------|-----------|-------|------------|
| T1 | Frontmatter: parse `groups:` from AGENT.md fixture (2 groups); merge scalar-replacement (override replaces base, not union); discovery shadowing outright-take | internal/agents/parser_groups_test.go, internal/agent/merge_groups_test.go | Groups field absent |
| T2 | User.Groups store round-trip + CLI flag shape (parse only) | internal/auth/user_groups_test.go | field absent |
| T3 | EffectiveGroups join matrix (nil identity / identity / dedup / stable order / NO key-string synthesis) | internal/groups/effective_groups_test.go | fn absent |
| T4 | Mutation gate e2e member: scratch daemon, grouped agent WITHOUT admin role calls groups.assign => denied; agent-update agent => applied + audit event; backstop: file_write config/agents/x.md => denied for non-admin | e2e/suites/agent-groups/ (member file) | RPC methods absent |

## Self-Verification Checklist

- [ ] All RED for missing-symbol reasons (record per-test evidence)
- [ ] T4 follows the scratch-home + live-RPC harness rules
- [ ] gofmt clean

**DO NOT COMMIT.** Orchestrator validates vs the branch Test Gate matrix,
commits, sets TESTS_VALIDATED.

## Completion Report Format

1. Files touched
2. RED evidence per test
