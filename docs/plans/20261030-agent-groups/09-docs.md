# 09: Documentation - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks. Do NOT commit.
> Explore with search_files/cat (no read_file on source). Write once, stop.

## Meta

- **Parent:** ../master.md
- **Scope:** The user-mandated docs deliverable (D6/R1): how it works, how to
  configure, how to diagnose. Written from the LANDED tree (last leaf).
- **Dependencies:** ALL other children COMPLETE
- **Estimated Context:** 50K
- **Concurrency Group:** E (last)

## Goal

An operator who has never seen this tree can configure groups, predict what
an agent can do, and diagnose a denial - from docs alone.

## Deliverables

1. `docs/workflows/agent-groups.md` - the feature spec:
   - Concepts: Policy / Binding / Subject / Scope, one-level nesting.
   - How it works: the THREE enforcement gates (dispatch intersection,
     call-time check, retrieval-time memory ceiling) + where fence/decision
     log fit; precedence rules WITH worked examples (deny-beats-allow,
     most-restrictive-wins, intersection-never-widens, D8 empty-allowlist,
     D7 ungrouped compatibility).
   - How to configure: config template section verbatim + frontmatter
     `groups:` + users.json5 `groups:` + overrides file + admin keys
     (security.groups.admin_agents/admin_users) - each with a copy-paste
     example.
   - Membership governance: who may mutate (agent-update agent + admin
     users), the file backstop, what a non-privileged attempt looks like.
   - Diagnosing permissions: `meept groups explain/check`, doctor check,
     decision log line anatomy (C2 keys + rule_source vocabulary table).
   - Overrides: add/list/prune + expiry semantics.
   - Edge cases table: the R5 list (cycles, dangling names, unknown groups,
     deny+allow same tool, Managed-empty, expired overrides).
2. `docs/configuration/agent-groups.md` (or section in the existing config
   reference per repo layout) - every config key, type, default, since when.
3. `docs/agents/opt-in-defaults.md` - record D7 (default_managed=false) and
   D8, citing this plan as the product-decision authorization.
4. `mkdocs.yml` - AUTO-NAV (verified: no manual Workflows section; do NOT add
   nav links; the doc alone ships).
5. Verify `make docs-generate` output is fresh for any gendoc-tagged config
   added by leaves 02/04 (run it; report drift).

## Tasks

| Task ID | Objective | Verify | PASS state |
|---------|-----------|--------|------------|
| T1 | docs/workflows/agent-groups.md per outline above | sections exist; examples match LANDED code (config keys verbatim from schema.go) | doc complete |
| T2 | Configuration reference page/section | keys match schema.go exactly (grep-verified) | complete |
| T3 | opt-in-defaults entries (D7, D8) | present, citing master.md | present |
| T4 | ASCII-normalize all new .md (pre-commit-ascii gate); no em-dashes | `bash .githooks/pre-commit-ascii <files>` passes | clean |
| T5 | Cross-check: every CLI command documented exists in the binary (`go run ./cmd/meept groups --help`) | output matches docs | matches |

## Self-Verification Checklist

- [ ] Every config key in docs grep-verifies against schema.go (no invented keys)
- [ ] Every documented command exists (help output pasted into report)
- [ ] Worked examples follow the FROZEN precedence exactly
- [ ] ASCII clean; mkdocs auto-nav respected

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Command-help evidence: [groups --help output]
3. gendoc drift: [none / regenerated]

## Review Checklist (For Review Agent)

- [ ] An operator could configure a working group from the doc alone
- [ ] Zero invented keys/commands (all verified against landed code)
- [ ] Edge-case table complete vs R5
