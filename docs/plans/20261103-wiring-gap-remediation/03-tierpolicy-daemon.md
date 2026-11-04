# Tierpolicy Daemon Wiring - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Close the tierpolicy daemon gaps: real cutoff, claim-source
  adapter, memory.tiers schema knob, working-store producer, engine call
  site. LARGEST leaf - coordinate with memory second-brain leaf
  03-agent-recall-wiring (another session queued it; re-grep before
  dispatch).
- **Dependencies:** schema knob (T1) before call site (T4); cutoff (T2)
  reads the knob.
- **Estimated Context:** 95K

## Goal

See parent master.md traceability table (this leaf's Req row).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Schema: MemoryConfig.Tiers knob (internal/config/schema.go MemoryConfig ~:1712+) + normalize + template + drift annotation (tiers_engine.go TiersConfigAdapter consumes it). | Modify: internal/config/schema.go, config/meept.json5, new config test | `go test -p 2 ./internal/config/ -run Tiers` | knob absent | roundtrip + defaults pinned | Given memory.tiers block; When loaded; Then adapter consumes it |
| T2 | Cutoff fix: tiers_engine.go:72 GetOldMemories(ctx, time.Time{}, limit) -> real cutoff (now - retention, from T1 knob). Test: zero-time returns nothing, real cutoff returns rows. | Modify: internal/daemon/tiers_engine.go + test | `go test -p 2 ./internal/daemon/ -run Tier` | zero cutoff matches nothing | candidates return | Given old memories; When tick runs; Then candidates non-empty |
| T3 | DemotionClaims adapter: epistemic claim lookup (epistemic.go:207 StoreClaim family) onto Deps.DemotionClaims; demotion fails-closed skip today (demote.go:129). | Modify: internal/daemon/tiers_engine.go + test | same | demotion skips | demote fires via adapter | Given stale claims; When Evaluate; Then demote actions planned |
| T4 | Engine call site: BuildTierPolicyEngine at ~components.go:2916 (after DistillWorker) + EngineHandle Start/Stop at shutdown. | Modify: internal/daemon/components.go, daemon.go if stop needed | `go test -p 2 ./internal/daemon/ -run TestTierEngineWiring` | no call site | enabled cfg -> engine started | Given enabled cfg; When boot; Then engine non-nil + started; disabled -> nil |
| T5 | Working-store producer: WireContextAssembler builds working.Store but nothing Appends. Producer at loop turn-end or extend daemon memory_capture (choose per second-brain leaf 03 state; document choice). | Modify: internal/daemon/context_assembler_wiring.go or memory capture path + test | `go test -p 2 ./internal/daemon/ -run WorkingProducer` | ring never grows | turn -> ring grows | Given a turn; When completed; Then working ring LiveTokens > 0 |

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] scoped tests green (-p 2)
- [ ] gofmt clean, ASCII only
- [ ] wiring-failure proof demonstrated
- [ ] sibling files untouched

## Review Checklist (orchestrator)

- [ ] Wiring point matches the survey table
- [ ] Proof test fails with wiring deleted (witnessed)
- [ ] No silent scope expansion
- [ ] Staged-set audit before commit

## Do NOT commit. The orchestrator stages and commits after review.
