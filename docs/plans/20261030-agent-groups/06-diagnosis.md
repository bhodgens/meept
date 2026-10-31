# 06: Diagnosis Surfaces (explain / check / doctor) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../master.md
- **Scope:** `meept groups explain <agent>`, `meept groups check`,
  doctor permissions check. All REPORT-ONLY (user norm: doctor never mutates).
- **Dependencies:** 02-groups-model, 04-tool-gates (REVIEWED - explain must
  read the SAME resolution the gates enforce)
- **Estimated Context:** 55K
- **Concurrency Group:** D

## Goal

"Group X denies shell for agent Y - which binding did that?" is one command.
Config defects (cycles, dangling names, unknown groups, empty-allowlist
surprises, expired overrides) are one `check` away.

## Interface Contracts (From Parent)

- C2/R4: explain output names, per grant/deny: the binding + policy that
  produced it. Explain calls the SAME internal/groups.Resolve the gates use -
  never a parallel implementation (drift hazard).
- R5 edge-case list (check must detect ALL): cyclic group membership,
  dangling policy ref, dangling tool name (via C6 validator), unknown group
  in frontmatter (C4), deny+allow same tool in one policy (report: deny wins),
  Managed+empty allowlist (report: agent will be denied EVERYTHING), agent in
  no group with default_managed=true (report: will run under `default`
  policy), expired/active overrides (C8 reader output).
- Exit codes: check exits 0 clean, 1 defects (scriptable).

## Verified Anchors

- CLI patterns: cmd/meept/agents_cmd.go, users_cmd.go, projects.go - follow
  agents_cmd.go shape for `meept groups` subcommands + root registration.
- RPC: internal/rpc/proxy.go handler registration + internal/services/ service
  layer conventions (service shared by RPC + HTTP; HTTP handler optional this
  leaf, service REQUIRED).
- Doctor: `meept doctor` report-only checks pattern (meept-development skill:
  doctor is report-only; installs are opt-in - this check installs nothing).
- explain needs agent discovery: reuse the agents registry/discovery read
  path (internal/agents discovery) - read-only.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | GroupsService (list/explain/check logic; reads config + registry + overrides reader), RPC handlers groups.list/groups.explain/groups.check | internal/services/groups_service.go, internal/rpc/groups.go, proxy.go registration, +tests | `go test -p 2 ./internal/services/ ./internal/rpc/ -run TestGroups` | absent | explain names binding+policy per entry; check enumerates ALL R5 defect classes with names |
| T2 | CLI: `meept groups list|explain|check` (check exit codes; human-readable + --json) | cmd/meept/groups.go, root cmd registration, +test | `go build -o bin/meept ./cmd/meept && ./bin/meept groups check` (fixture config) | subcommand absent | explain table render; check exit 1 on injected defect, 0 clean |
| T3 | Doctor permissions check: groups config validity + count of agents whose dispatch-visible tools diverge from Resolved.AllowedTools (should be structurally zero; catches wiring drift) | doctor check site, +test | `go test -p 2` doctor pkg | check absent | report-only output; divergence detection exercised via injected fake |
| T4 | --json shapes for explain/check pinned (stable keys) | service tests | `go test -p 2 ./internal/services/` | n/a | golden JSON keys test |

## Self-Verification Checklist

- [ ] explain/check call groups.Resolve - ZERO duplicated precedence logic (grep)
- [ ] check covers every R5 edge case (table test per class)
- [ ] Doctor check report-only; no install prompts
- [ ] gofmt clean; gate tests GREEN and unmodified

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each; include sample explain + check output]
3. Deviations: [none / list]

## Review Checklist (For Review Agent)

- [ ] Single resolution path shared with enforcement (the drift trap)
- [ ] Exit codes scriptable; JSON keys stable and tested
- [ ] All eight R5 edge cases have a named check with a test
