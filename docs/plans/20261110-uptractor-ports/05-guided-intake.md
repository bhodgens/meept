# Guided Intake (Secretary) — Implementation Leaf (05)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks using TDD. The failing gate tests ALREADY EXIST — make
> them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT
> commit. Do NOT use read_file on existing source files — explore with
> search_files or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** ./master.md
- **Scope:** Guided-intake call behavior for the secretary employee: identify caller, one question at a time, read-back, confirm-then-save, refuse-and-list on ambiguous fuzzy matches.
- **Dependencies:** leaf 03 (fuzzy) COMPLETE; leaf 04 (brief/commitments) recommended but not blocking
- **Estimated Context:** 80K
- **Concurrency Group:** C

## Goal

Port the guided-intake CALL BEHAVIOR from ~/git/uptractor-jobfile branch
fix/coordinator-missed-hangup-watchdog (phone/core/README-intake.md +
phone/core/internal/agent/intake.go, intake_record.go) as a meept employee
behavior preset — generic, no Uptractor jobs/work-orders vocabulary. The
secretary employee (owner decision: "Port to meept for the secretary") runs
an intake call as: identify the caller (callerid seam), find out what they
need, collect missing fields ONE question at a time, read the record back, and
save only after explicit confirmation. Any registry resolution (contacts,
ledger subjects) goes through fuzzy (C5): exact match wins; single fuzzy
candidate above threshold proposes "did you mean X?"; two or more candidates
REFUSE and list them so the agent asks the caller; below threshold => offer to
create new (subject to the employee's constitution/tools_allowed).

## Context

- internal/callerid/ — Info{Number,Name,Known,Approved} stamp already on every voice turn ctx
- internal/employee/ — constitution, tools_allowed, presets (locate via grep ApplyPreset)
- internal/ledger/ — subjects registry (dispatch-board Q1: canonical key exists before filing)
- internal/contact/store.go — contacts registry for caller resolution
- Source behavior spec: cat phone/core/README-intake.md (both behaviors; only guided intake ports here — set_work_order_status is Uptractor-domain and stays out)

## Verified Anchors

- internal/callerid/callerid.go — Info + With/From transport (voice stamps, tools read)
- internal/ledger/store.go:316 — Append (save path for a confirmed intake record)
- internal/util/fuzzy/fuzzy.go (leaf 03 output) — FuzzyFind

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/employee (or internal/intake if employee pkg rejects the dep — orchestrator decides)
type IntakeField struct {
    Name     string   // canonical field name
    Question string   // spoken prompt when missing
    Required bool
}
type IntakeSpec struct {
    Kind   string       // e.g. "message", "change_request" — becomes ledger subject vocabulary
    Fields []IntakeField
}
type ResolveOutcome int
const (
    ResolveExact ResolveOutcome = iota
    ResolveFuzzySingle   // propose confirmation
    ResolveAmbiguous     // REFUSE + list candidates (never auto-pick)
    ResolveNoMatch       // offer create-new path
)
func ResolveSubject(query string, candidates []string, threshold float64) (outcome ResolveOutcome, match string, matches []string)
```

Config: `voice.guided_intake` bool on the employee record (O2 rec). When on,
the employee's voice system prompt is extended with the intake procedure and
an `intake_save` tool exists that requires a confirmed record payload; the
tool appends to the ledger (kind per spec, source=voice, source_ref=ccid).
Saving without a prior read-back+confirm turn is refused by the tool (the
tool checks a confirm flag set only by an explicit confirmation turn).

### What This Leaf Consumes

- C5 fuzzy (leaf 03). Gate tests: internal/voice/guided_intake_test.go.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---|---|---|---|---|---|---|
| T1 | ResolveSubject over fuzzy with the four outcomes + threshold 0.60 | Create internal/intake/resolve.go | `go test ./internal/intake` | gate RED | green | Given candidates ["Meadow Ridge","Meadow Brook"] and query "meadow ridg e"; When resolve; Then Ambiguous with both listed |
| T2 | intake_save tool: schema-validated payload, confirm-flag gate, ledger append | Create internal/intake/tool.go; register in employee toolset | `go test ./internal/intake -run Save` | gate RED | green | Given no confirm turn yet; When tool called; Then refused with reason |
| T3 | Employee preset wiring: guided_intake flag -> prompt extension + tool grant; secretary default preset opts in | Modify internal/employee preset files (grep ApplyPreset) | `go test ./internal/employee` + build | n/a | green | Given secretary hired with guided_intake; When voice turn; Then intake prompt present |
| T4 | One-question-at-a-time prompt block: missing required fields drive the next question; read-back assembled from collected fields | Create internal/intake/prompt.go | `go test ./internal/intake -run Prompt` | gate RED | green | Given 2 of 3 fields collected; When prompt built; Then exactly the missing field's question |

## Self-Verification Checklist

- [ ] Ambiguous NEVER auto-picks (test asserts refusal, not best-effort)
- [ ] Save without confirm impossible at the tool layer (not just prompt layer)
- [ ] No Uptractor vocabulary (jobs/work orders) — generic subjects only
- [ ] `go test -p 2 ./internal/intake ./internal/employee ./internal/voice` green
- [ ] tools_allowed still governs: intake_save absent for employees without the preset

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** 2. **Verify command + output:** last 20 lines 3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] Confirm-gate is enforced in code, prompt-only enforcement rejected
- [ ] Fuzzy thresholds match C5 frozen values
- [ ] callerid Info consulted for identify-caller step (Approved bit respected)
- [ ] Ledger appends use DraftRecord correctly (kind/source/source_ref)
- [ ] No debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- The dispatch-board tree (separate, already ratified) will later consume
  intake records as dispatch_file inputs; keep IntakeSpec.Kind vocabulary
  open (string, not enum) so the board can extend it without touching this pkg.
