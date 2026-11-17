# Docs and topology refresh - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Do NOT edit generated
> files by hand. Do NOT commit. Do NOT use read_file on existing source files;
> explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Regenerate graphs; update bots.md and coordination doc
- **Dependencies:** 01-daemon-wiring.md
- **Estimated Context:** 35K
- **Concurrency Group:** B

## Goal

Make generated topology reflect BOTH `bot.result` and `employee.result` and update the two docs that
currently describe bots as retired.

## Context

- `docs/workflows/bots.md:1-5` - currently says deprecated, commands removed
- `docs/workflows/inter-agent-coordination.md` - #139 convergence doc
- `docs/generated/bus-topology.*` - generated; `make graphs` rewrites them
- Root AGENTS.md - "every commit reviews this file" rule

## Verified Anchors

- `docs/workflows/bots.md:1` - `# Persistent Bot Framework (deprecated)`
- `internal/comm/http/server.go:762` - existing typed topic subscription entry
- `docs/workflows/inter-agent-coordination.md:7-9` - convergence list

## Interface Contracts (From Parent)

### What This Leaf Exposes

- updated topology with `bot.result` AND `employee.result` publisher/subscriber edges
- updated `bots.md` and coordination doc

### What This Leaf Consumes

- C1 topic names `bot.result` + `employee.result` (owner: branch 01)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Regenerate graphs | `docs/generated/*` | `make graphs && make graphs-check` | n/a | check green | Given two new topics; When graphs regenerated; Then bot.result AND employee.result appear in JSON+MD |
| T2 | Update bots doc | Modify: `docs/workflows/bots.md` | `grep -n 'result event\|inbox' docs/workflows/bots.md` | absent | present | Given doc; When read; Then describes active tier + new capabilities |
| T3 | Update coordination doc | Modify: `docs/workflows/inter-agent-coordination.md` | `grep -n 'bot.result\|bots' docs/workflows/inter-agent-coordination.md` | absent | present | Given doc; When read; Then both tiers listed as converging surfaces |

T2 steps: rewrite `bots.md` to state both tiers are supported; document that
bots emit `bot.result`, employees emit `employee.result`, both emit
`agent.turn.state`, and bots accept `send_agent_message`.
Keep the history note that `meept bots` commands were removed, but explain the
runtime is live via `bots.enabled=true` and employee wiring.

T3: add a short section naming BOTH `bot.result` and `employee.result` and the two emitters, referencing
issue #140 and the frozen C1 shape.

## Self-Verification Checklist

- [ ] `make graphs-check` green; no hand-edited generated file
- [ ] Docs accurate; lowercase UI text
- [ ] Root AGENTS.md reviewed for staleness (no new package or topic contract beyond this tree)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Generated files consistent with source
- [ ] Docs match implemented behavior exactly

Output: APPROVED or specific gaps.

## Notes

`docs/generated/*` is regenerated, never edited by hand.
