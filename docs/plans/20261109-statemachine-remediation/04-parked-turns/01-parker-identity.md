# Parker Identity (C1 Keystone) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks with TDD. Gate tests exist and fail RED. Do NOT commit. Do NOT use read_file on existing sources.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** THE C1 FIX: park rows carry their owner; Load returns only owned rows; payload decode tolerance tightened (no cross-class misroute).
- **Dependencies:** 00T-park-tests.md
- **Estimated Context:** 55K
- **Concurrency Group:** A (alone — everything depends on it)
- **Audit references:** C1 (CRITICAL), F8 (stash), C2 contract in master

## Goal

After restart, each persisted parked turn resumes exactly once, by exactly
its owning parker. The quota payload decoder cannot misroute foreign rows.

## Context

- internal/agent/park_store_sqlite.go: Load kind-scoped only (:205-209), Save (:171 created_at), parseParkClass silently maps unknown classes (:337-345)
- internal/agent/parked_turn.go: TurnParker (SetParkPersistence, drainDue delete-before-callback)
- internal/agent/quota_resume.go:310-317 SetParkPersistence(story, ParkKindChat); adapted callback json.Unmarshal into quotaTurnPayload (:177-190) — overlapping keys make foreign rows decode "successfully"
- internal/daemon/components.go:4286-4318: quota watcher parker + throttleParker, BOTH SetParkPersistence(ParkKindChat)

## Verified Anchors

Re-grep at dispatch.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C-K1:
// ParkedTurnRecord gains Owner string (persisted column `owner`, ADD COLUMN migration + backfill 'quota-watcher' for existing rows).
func (p *TurnParker) SetPersistenceOwner(owner string) // stamps every Save; Load filters rows WHERE owner = ?
// parseParkClass: unknown class -> drop row with error log (was: silent FailureQuota).
// quota callback decode: quotaTurnPayload gains strict decode (unknown/malformed payload -> drop + warn, NEVER resume).
```

### What This Leaf Consumes

- ParkPersistence interface (existing); owners are plain strings agreed at wiring: "quota-watcher", "throttle", "gate", "episode".

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Schema + owner column | Modify: internal/agent/park_store_sqlite.go (ADD COLUMN owner + backfill; index on kind,owner) | `go test -p 2 ./internal/agent/ -run TestParkStore` + legacy-DB open test (new) | absent | GREEN: fresh + migrated stores carry owner; legacy rows backfilled | Given pre-migration parks.db; When opened; Then rows owner='quota-watcher', store functional |
| T2 | Owner stamp + filter | Modify: parked_turn.go (SetPersistenceOwner, Save stamps, Load filters) | gate TestLoadReturnsOnlyOwnedRows GREEN | RED | GREEN | Given two parkers, distinct owners, shared store; When both Load; Then disjoint row sets |
| T3 | Wiring owners | Modify: internal/daemon/components.go (quota watcher owner="quota-watcher", throttleParker owner="throttle") + gate_park wiring ("gate") + episode wiring ("episode") | `go build ./...` + gate TestRestartRearmSingleOwner GREEN | RED | GREEN: restart re-arm routes each row to exactly one parker; total resumes across parkers == 1 per row | Given 1 throttle + 1 quota row persisted; When parkers restart and re-arm; Then each resumed by owner only, once |
| T4 | Decode strictness | Modify: quota_resume.go callback + park_store_sqlite.go parseParkClass | `go test -p 2 ./internal/agent/ -run TestQuotaPayloadStrictDecode` (new) | tolerant decode | GREEN: throttle payload into quota callback -> dropped with warn; unknown class row -> dropped | - |
| T5 | Stash isolation (F8) | Modify: loop_park.go throttleTurnCtx stash -> per-turn capture (keyed by turn id or captured at park call site) | gate TestThrottleStashPerTurn GREEN | RED | GREEN: overlapping turns park their own payloads | - |
| T6 | AGENTS.md seam note | Modify: internal/agent/AGENTS.md (owner contract one-liner) | grep | - | documented | - |

## Self-Verification Checklist

- [ ] C1-killing test GREEN (T3 restart shape)
- [ ] Migration backward-compatible (existing parks.db opens)
- [ ] No row can resume twice across parkers (count assertion in test)

**DO NOT COMMIT.**

## Completion Report Format

Files; verify tail; deviations.

## Review Checklist (For Review Agent)

- [ ] Contract C-K1 exact; owner strings consistent wiring-wide
- [ ] Decode failures DROP (never misroute)
- [ ] internal/agent tests + daemon build green

## Notes

This leaf must land alone before 02/03/04 (they build on owner semantics).
The C1 double-execution window is CLOSED by T2+T3 even before decode
strictness; T4 is defense in depth.
