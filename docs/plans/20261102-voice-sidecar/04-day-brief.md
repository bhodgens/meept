# Day brief - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate test ALREADY EXISTS
> (parked .staged from leaf 00T T7) - make it pass; do NOT modify gate
> tests. Do NOT commit. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** C7 DayBrief: local-midnight activity window, prior call transcripts today, capped durable memory, active goals, prompt block
- **Dependencies:** none (gate test 00T T7)
- **Estimated Context:** 55K
- **Concurrency Group:** A
- **Files this leaf owns:** internal/voice/daybrief.go

## Goal

Build the text block that gives a call the day's prior context. Injected
into the employee prompt at answer/dial time (leaf 12 consumes
PromptBlock()). The model never has to remember to search; memory_search
has no time filter and is NOT used here.

## Context

- internal/activity: Aggregator.Query(ctx, Query{Since, Until, Kinds, Limit})
  returns []ActivityRecord (Summary <=200 runes each by contract C1 of that
  package). Read internal/activity/{types.go,sources.go,aggregator.go} via
  terminal cat for exact shapes.
- Calendar convention (repo defect class): ONE convention per day-keyed
  store - use LOCAL midnight (time.Now().In(loc) truncated to day), never
  a UTC-derived day key. The gate test runs a TZ differential.
- Prior call transcripts: the call-session store lands in leaf 12; this
  leaf takes a narrow interface CallTranscriptSource (list transcripts for
  number+day) so it compiles standalone. Leaf 12 provides the real source.
- Durable memory: internal/memory Manager search by fixed keys (name,
  preferences, standing rules) with a hard rune cap. Narrow interface
  MemorySource, same reason.
- Active goals: narrow interface GoalSource wrapping the employee
  GoalStore ListActive shape (do NOT import internal/employee - it imports
  agent; keep voice dependency-free and inject at wiring).

## Verified Anchors

- internal/activity/sources.go:10-21 - Query struct (Since/Until/Kinds/Limit)
- internal/activity/types.go:10-22 - ActivityRecord (Summary <=200 runes)
- master.md C7 - DayBrief shape verbatim

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type DayBrief struct { Activity []activity.ActivityRecord; PriorCallTranscripts []CallTranscript; DurableMemory string; ActiveGoals []string }
type CallTranscript struct{ Number string; At time.Time; Summary string }
type CallTranscriptSource interface{ TodayTranscripts(ctx context.Context, number string) ([]CallTranscript, error) }
type MemorySource interface{ DurableBlock(ctx context.Context) (string, error) }
type GoalSource interface{ ActiveGoalTitles(ctx context.Context, employeeID string) ([]string, error) }
func Build(ctx context.Context, deps Deps, employeeID, number string, now time.Time, loc *time.Location) (DayBrief, error)
func (b DayBrief) PromptBlock() string  // <=2000 runes total, sections labeled
```
Deps carries the three narrow sources + activity aggregator; nil source =
section omitted (typed-nil guard), never an error.

### What This Leaf Consumes

- C7 (master.md); activity.Query/ActivityRecord (existing package)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate test; confirm RED | mv internal/voice/daybrief_test.go.staged -> daybrief_test.go | go test -p 2 ./internal/voice/ -run TestDayBrief | undefined: Build | RED confirmed | - |
| T2 | Build(): local-midnight window; activity capped (Limit from const, default 20); nil sources omit sections | Create: internal/voice/daybrief.go | go test -p 2 ./internal/voice/ -run TestDayBrief | - | PASS | Given row at 23:30 local yesterday and now=00:30 local; When Build; Then row excluded (TZ differential both ways) |
| T3 | PromptBlock(): labeled sections, total <=2000 runes, per-summary <=200 runes enforced on output | (same) | go test -p 2 ./internal/voice/ -run TestDayBriefBlock | - | PASS | Given oversized inputs; When PromptBlock; Then <=2000 runes, sections present in fixed order |
| T4 | Format/vet | - | gofmt -l internal/voice; go vet ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] Gate test (TZ differential) passing untouched
- [ ] No memory_search usage; no internal/employee import
- [ ] Local midnight, never UTC-derived
- [ ] Nil-source paths never error, never panic
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] C7 shape exact; caps enforced in code, not just tests
- [ ] TZ differential genuinely runs under both zones (t.Setenv, not a comment)
- [ ] Narrow interfaces keep voice import-free of employee/memory internals

Output: APPROVED or specific gaps.

## Notes

Activity summaries are already envelope-stripped <=200 runes by the
activity package contract; re-clamp defensively at PromptBlock only.
