# Trace-Fed Evolver + Impact Ledger — Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing a
> file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ./master.md
- **Scope:** Evolver Pass A consumes sampled traces + wiki index + the
  skill-impact ledger; every verifier verdict (accept AND reject) is appended
  to skill-impact.md (arXiv:2608.27454 §3.2.2-§3.2.4).
- **Dependencies:** 01-trace-store.md (TraceProvider interface),
  02-wiki-store.md (WikiStore + ReadSkillImpact). Interfaces only — both
  leaves may still be IN_PROGRESS; code against the frozen contracts.
- **Estimated Context:** ~55K
- **Concurrency Group:** B
- **Research reference:** arXiv:2608.27454 (WikiSkill) §3.2 maintainer +
  proposer + gating; Appendix C sampling budgets. Commit message MUST cite
  the arXiv id.

## Goal

Today Pass A refine prompts carry only usage counters
(`inject_count=%d, positive=%d...`, evolver.go:277-286) and rejected
proposals are forgotten — the next cycle can propose the same failed edit
again. This leaf makes Pass A wiki- and trace-informed and turns the evolver
into the maintainer of `skill-impact.md`:

1. Refine prompt prepends: skill-impact.md (with "do not repeat rejected
   proposals" instruction), then wiki/index.md, then sampled traces
   (5 fail + 3 pass, 15k chars each).
2. `processProposal` appends a `SkillImpactEntry` for EVERY verifier verdict —
   accept AND reject — recording candidate content as the diff payload.

The verifier, Writer, Versioner, plan-approval flow, and all four passes
remain otherwise unchanged. Passes B/C/D are untouched.

## Context

Key files to understand before implementing:

- internal/skills/lifecycle/evolver.go — `Evolver` struct (65), options
  (79-103), `RunCycle` (149), `passARefine` (195), `buildRefinePrompt` (277),
  `processProposal` (530). Tests in evolver_test.go share
  `newTestVerifier` + `mockLLMChatter` — reuse, grep before redeclaring.
- internal/skills/lifecycle/types.go — `EvolutionProposal` (VerifierResult
  field exists at 169), `EvolutionReport`.
- internal/selfimprove/wiki.go + trace_store.go — the frozen contracts
  (master §Contract 2/§Contract 1). This leaf adds NO methods to those types.
- internal/config/schema.go:1829 — SkillsEvolverConfig. This leaf adds NO
  config fields (sampling budgets are package constants per paper).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/skills/lifecycle/evolver.go (or new file evolver_wiki.go)
package lifecycle

// TraceProvider decouples the evolver from the concrete store (tests stub it).
// Satisfied structurally by *selfimprove.TraceStore — define locally so the
// lifecycle package does not hard-import selfimprove for the interface
// (the WikiStore option below does import selfimprove for the concrete type).
type TraceProvider interface {
    Sample(maxFails, maxPasses, maxChars int) ([]selfimprove.TraceRecord, error)
}

func WithTraceProvider(tp TraceProvider) EvolverOption      // nil guard
func WithWikiStore(ws *selfimprove.WikiStore) EvolverOption // nil guard

const (
    traceSampleMaxFails  = 5
    traceSampleMaxPass   = 3
    traceSampleMaxChars  = 15000
    impactLedgerMaxChars = 20000
)

// appendSkillImpact: JSONL row via WikiStore.AppendSkillImpact.
// Called from processProposal for every Verify verdict; wiki==nil is a no-op.
```

### What This Leaf Consumes

- `selfimprove.TraceRecord`, `selfimprove.WikiStore` (leaf 01/02).
- Existing `Evolver` fields: `verifier`, `llmClient`, `usage`, `registry`,
  `writer`, `cfg`, `logger`.

## Tasks

### Task 1: Options + struct fields

**Objective:** Add traceProvider + wiki fields with nil-guarded options.

**Files:**
- Modify: `internal/skills/lifecycle/evolver.go` (struct + option block)
- Create: `internal/skills/lifecycle/evolver_wiki.go` (preferred for pass-A
  prompt builders + ledger code)
- Test: `internal/skills/lifecycle/evolver_wiki_test.go`

**Step 1: Write failing test**

```go
func TestEvolverWikiOptions_NilGuards(t *testing.T) {
    e := &Evolver{}
    WithTraceProvider(nil)(e)
    WithWikiStore(nil)(e)
    if e.traceProvider != nil || e.wiki != nil {
        t.Fatal("nil options must be no-ops")
    }
}
```

(If struct fields are unexported in a different file, put this test in
`package lifecycle` internal test file matching existing evolver_test.go style.)

**Step 2: Run test to verify failure**

Run: `go test ./internal/skills/lifecycle/ -run TestEvolverWikiOptions -v`
Expected: FAIL — undefined: WithTraceProvider

**Step 3: Write minimal implementation**

Two fields on Evolver (`traceProvider TraceProvider`, `wiki
*selfimprove.WikiStore`), two options with the repo's nil-guard shape (copy
WithEvolverLLMChatter at evolver.go:84). NOTE the import direction check:
internal/skills/lifecycle ALREADY imports internal/selfimprove
(learning pipeline param in NewEvolver) — no cycle.

**Step 4: Run test to verify pass**

Run: `go test ./internal/skills/lifecycle/ -run TestEvolverWikiOptions -v`
Expected: PASS

### Task 2: Trace-fed refine prompt

**Objective:** Pass A refine prompt carries ledger + index + sampled traces.

**Files:**
- Modify: `internal/skills/lifecycle/evolver.go` (passARefine + buildRefinePrompt)
- Test: `internal/skills/lifecycle/evolver_wiki_test.go`

**Step 1: Write failing test**

```go
func TestPassARefine_PromptCarriesLedgerTracesIndex(t *testing.T) {
    // stub wiki: ReadSkillImpact returns a known rejected row;
    // stub TraceProvider returning 1 failure + 1 success record;
    // stub LLM capturing the user prompt (mockLLMChatter already records? —
    // check; if not, extend the mock in the new test file, do not mutate the
    // shared one).
    // Assert the captured prompt contains, in order:
    //   "--- Prior Skill Impact (do not repeat rejected proposals) ---"
    //   the rejected row's skill name
    //   "--- Wiki Index ---"
    //   "--- Execution Traces ---"
    //   "[outcome=failure]"
}
```

Also test the degraded paths: wiki==nil + traces==nil ⇒ prompt is
byte-identical to today's `buildRefinePrompt` output (golden: usage stats
line still present first).

**Step 2: Run test to verify failure**

Run: `go test ./internal/skills/lifecycle/ -run TestPassARefine -v`
Expected: FAIL — prompt lacks new sections

**Step 3: Write minimal implementation**

- New `buildWikiContext()` in evolver_wiki.go: reads
  `wiki.ReadSkillImpact()`, truncates trailing content to
  `impactLedgerMaxChars` (keep the NEWEST rows — truncate the OLDEST when
  over cap; the newest rejections matter most), renders wiki/index.md via a
  small `ReadIndex()` call — ADD `func (w *WikiStore) ReadIndex() (string, error)`
  to selfimprove/wiki.go as part of this task (tiny sibling edit, allowed by
  this leaf; note it in Deviations so review checks it against Contract 2).
- Trace blocks: `[outcome=f|s] <id> <domain>\n<rendered steps>` per record,
  each capped by traceSampleMaxChars.
- Wire into `passARefine` before the per-skill loop: build the shared
  context ONCE per cycle (not per skill), pass into buildRefinePrompt as an
  extra param (keep old signature behavior when both nil).

**Step 4: Run test to verify pass**

Run: `go test ./internal/skills/lifecycle/ -race -count=1 -v`
Expected: PASS (whole package — existing evolver tests must stay green)

### Task 3: Impact ledger on every verdict

**Objective:** processProposal appends SkillImpactEntry for accept AND reject.

**Files:**
- Modify: `internal/skills/lifecycle/evolver.go` (processProposal, ~line 530)
- Test: `internal/skills/lifecycle/evolver_wiki_test.go`

**Step 1: Write failing test**

```go
func TestProcessProposal_RecordsRejectAndAccept(t *testing.T) {
    // dir := t.TempDir(); real selfimprove.NewWikiStore(dir, ...)
    // evolver with newTestVerifier(false) → run one proposal →
    // ReadSkillImpact must contain 1 row, Accepted=false, Action+SkillName set.
    // Switch to newTestVerifier(true), planMgr=nil, AutoApply=false →
    // second proposal → row 2 Accepted=true (verdict ledger ≠ apply outcome).
}
```

**Step 2: Run test to verify failure**

Run: `go test ./internal/skills/lifecycle/ -run TestProcessProposal_Records -v`
Expected: FAIL — skill-impact.md absent/empty

**Step 3: Write minimal implementation**

In processProposal immediately after `proposal.VerifierResult = vr` and the
accept/reject branch determination: call `e.appendSkillImpact(...)` with
Action=verifyAction, SkillName, Diff=CandidateContent (truncate at 4k chars
per row to keep the ledger sane), Score=vr.Score, Accepted=vr.Action ==
ActionAccept, Reason=strings.Join(vr.Reasons, "; "). Wiki nil ⇒ no-op.
The verifier-error branch (err != nil) records nothing (no verdict).

**Step 4: Run test to verify pass**

Run: `go test ./internal/skills/lifecycle/ -race -count=1 -v`
Expected: PASS

### Task 4: Self-verification sweep

- `go build ./...`
- `go test ./internal/skills/... -race -count=1` (lifecycle + integration
  test dir compiles)
- `gofmt -l` touched files
- `go run ./tools/analyzers/mutexio/... ./internal/skills/lifecycle/`
- Confirm: zero references to wiki/traces from internal/agent/prompts/ and
  context_injector.go.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`-race -count=1`)
- [ ] Contracts satisfied exactly (option names, constants, prompt sections)
- [ ] Existing evolver_test.go + integration skill_evolver_lifecycle_test.go
      remain green with nil wiki/traces
- [ ] Files at exact specified paths; no deviations (or documented)
- [ ] Ledger records BOTH verdicts; verifier-error path records nothing

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list with rationale — expected: ReadIndex
addition to wiki.go, flag it for cross-leaf review]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master §Contract 3 exactly
- [ ] Prompt degradation: nil wiki + nil traces ⇒ identical-to-today prompts
- [ ] Ledger truncation: newest rows kept when over impactLedgerMaxChars
- [ ] Passes B/C/D untouched (diff shows no changes to passB/C/D logic)
- [ ] No debug artifacts; gofmt clean; no unused exports

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The paper's Skill Proposer is a ReAct agent; meept's Pass A is a single
  prompt-and-JSON call. That is the deliberate v1 simplification (the evolver
  already runs unattended on a 6h scheduler) — do not build a ReAct loop.
- Sampling constants live in code, not config, per master Contract 3. If the
  user later wants them tunable, that is a config leaf, not this one.
- Do NOT touch ContextInjector. The wiki is evolver-only (paper §5.1).
