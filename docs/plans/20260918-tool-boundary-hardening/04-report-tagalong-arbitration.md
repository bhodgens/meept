# Report Tag-Along Arbitration - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat. After
> writing a file, do NOT read it back to verify. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Extend the work-plus-report compound collapse so "do X, then tell me/show me <the result>" routes as a single action instead of a compound pair session - without regressing the F42 genuine-second-deliverable protection.
- **Dependencies:** none
- **Estimated Context:** ~50K
- **Concurrency Group:** A
- **Audit references:** 2026-09-18 e2e T1 ("create a file named hello.txt in the current directory containing the word hello, then tell me the full path" → "Compound intent detected intents=4 type=parallel" → pair session actor=coder reviewer=planner → planner churn). Precedent: 2026-09-10 LLM arbitration (dispatcher.go:2547 comment) collapsed the chat-tag-along variant; F42 (2026-09-12) excluded report from chatLike because report is a second deliverable.

## Goal

The 2026-09-10 collapse (dispatcher.go:2628) absorbs chat/platform/recall
tag-alongs when exactly one actionable intent remains. `report` was excluded
(bughunt F42): "write a summary and a report" is two deliverables. But the
READBACK flavor of report - "then tell me the full path", "and show me the
result" - is not a second deliverable; it asks for the FIRST action's output
to be surfaced. This flavor still went compound in the e2e (4 intents, none
chatLike) and routed a one-file task into a planner pair session.

This leaf adds a positive-signal detector: a report clause that REFERENCES
THE FIRST ACTION'S OUTPUT (its result/path/output/content) is a readback and
collapses with the actionable intent. A report clause that describes an
INDEPENDENT artifact (its own topic, its own verb: "write a report about X")
stays compound - F42's protection.

## Context

- `internal/agent/dispatcher.go` `classifyMultiIntent` (~:2565): builds
  `multi.Intents`; the collapse block at ~:2628 counts chatLike vs
  actionable intents above `compoundIntentConfidenceFloor`.
- The T1 prompt: "create a file named hello.txt in the current directory
  containing the word hello, then tell me the full path" - clause splitter
  and signal words live in the same file (find `hasCompoundSignalWords`,
  the "then" handling, and how the compound path splits clauses - search
  `Compound intent detected` producer to find the split point).
- Log line: `Compound intent detected component=dispatcher intents=4 type=parallel`.

Key files:
- `internal/agent/dispatcher.go` - classifyMultiIntent, the collapse block,
  clause splitting
- `internal/agent/dispatcher_*_test.go` - existing compound/arbitration pins
  (dispatcher_platform_arbitration_test.go, dispatcher_compound_misroute_test.go)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/dispatcher.go (or dispatcher_report_tagalong.go - new file preferred)
package agent

// reportTagAlongVerbs: readback verbs that ask for an artifact's surfacing.
// tell|show|print|display|give|list|report back
// reportTagAlongObjects: the first action's OUTPUT references.
// path|paths|result|results|output|content|contents|it|that|them|file

// classifyReportTagAlong reports whether the REPORT clause asks to surface
// the FIRST action's output rather than produce an independent deliverable.
// Positive signals (any): object noun from the list above; demonstrative
// pronoun (it/that); possessive reference to the action artifact
// ("the file('s) ... path"). Negative signals (any ⇒ false): the clause has
// its OWN work verb (write|create|make|generate|produce|summarize <new
// object>) or introduces a NEW topic noun not present in the input's
// actionable clause.
func classifyReportTagAlong(actionClause, reportClause string) bool
```

Collapse integration in `classifyMultiIntent`: extend the existing block -
when `actionable == 2` and the lower-confidence intent is `report` AND the
clause split yields `classifyReportTagAlong(actionClause, reportClause)`,
set `multi.IsCompound = false` (route to the single actionable intent), log
"Compound arbitration: report readback collapsed" with both clauses'
truncated text. Leave `actionable >= 3` untouched. Leave chatLike collapse
as-is.

### What This Leaf Consumes

- The clause-splitting mechanism the compound detector already uses (find how
  intents map back to input clauses; if no clause map exists, split on the
  signal connector - "then", ", and" - for the two-intent case only).

## Tasks

### Task 1: classifyReportTagAlong predicate

**Objective:** Pure classifier with positive/negative pins.

**Files:**
- Create: `internal/agent/dispatcher_report_tagalong.go`
- Test: `internal/agent/dispatcher_report_tagalong_test.go`

**Step 1: Failing tests** (table-driven):

```go
func TestReportTagAlong_Positive(t *testing.T) {
    // action: "create a file named hello.txt ... containing the word hello"
    // report: "then tell me the full path" -> TRUE
    // report: "and show me the result" -> TRUE
    // report: "then tell me where it is" -> TRUE (demonstrative it)
}
func TestReportTagAlong_IndependentDeliverable(t *testing.T) {
    // action: "write a summary of the doc"
    // report: "then write a report about the findings" -> FALSE (own work verb + new topic)
    // F42 shape: "create the config and write a report" -> FALSE
}
func TestReportTagAlong_NoWorkVerbNeeded(t *testing.T) {
    // report clause "the full path please" -> TRUE (object reference, no verb)
}
func TestReportTagAlong_NewTopic(t *testing.T) {
    // action: "fix the flaky test"
    // report: "then tell me the weather" -> FALSE (new topic, no artifact reference)
}
```

**Step 2:** FAIL. **Step 3:** Implement (case-insensitive; keep the word
lists small and comment each entry with the e2e prompt it earns its keep
from). **Step 4:** PASS.

### Task 2: Collapse integration

**Objective:** Two-intent actionable+report-readback requests route single.

**Files:**
- Modify: `internal/agent/dispatcher.go` (collapse block ~:2628)
- Test: `internal/agent/dispatcher_report_collapse_test.go`

**Step 1: Failing tests:**

```go
func TestClassifyMultiIntent_ReportReadbackCollapses(t *testing.T) {
    // T1 prompt verbatim; classifier stubs emitting [code@0.9, report@0.8]
    // (plus the two others the real run saw, e.g. chat@low filtered by floor)
    // -> multi.IsCompound == false; summary/single intent = code
}
func TestClassifyMultiIntent_ReportDeliverableStaysCompound(t *testing.T) {
    // "create the config and write a report" [code@0.9, report@0.85] -> still compound
}
func TestClassifyMultiIntent_ThreeActionableUntouched(t *testing.T) { // actionable==3 -> compound (no change) }
```

Drive through `classifyMultiIntent` with stubbed keyword/LLM classifiers the
way `dispatcher_compound_misroute_test.go` does (reuse its fixtures).
**Step 2:** FAIL. **Step 3:** Implement the extended collapse (Contract
shape; log line added). **Step 4:** PASS.

### Task 3: Regression sweep (F42 + 2026-09-10 arbitration)

**Objective:** Prior collapse pins unregressed.

**Files:**
- Run: existing compound/arbitration tests

**Step 1:** `go test -p 2 -count=1 ./internal/agent -run 'TestCompound|TestClassifyMultiIntent|TestArbitration|TestReport' -v`. **Step 2:** any regression → narrow (the readback detector is too loose; tighten per fixture). **Step 3:** green. List every pre-existing pin name that exercises the collapse block in your report with PASS/FAIL.

## Self-Verification Checklist

- [ ] T1 prompt verbatim collapses to a single intent (pin)
- [ ] F42 shape stays compound (pin)
- [ ] chatLike collapse behavior byte-identical (pre-existing pins green)
- [ ] Word lists documented with motivating prompts
- [ ] go build ./... clean; gofmt clean; package suite green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Positive signals are POSITIVE (object/demonstrative reference to the
      action's output), not just "report clause exists"
- [ ] Own-work-verb negative signal present and pinned
- [ ] Collapse fires only for actionable==2 with report second
- [ ] F42 pin named and green
- [ ] No changes to DetectCompound or the confidence floor

Output: APPROVED or specific gaps with file:line.

## Notes

- T1 evidence: log line "Compound intent detected intents=4 type=parallel" at
  18:07:51 (kept workdir daemon.log, meept-e2e.p5ZmMR). The intents=4 count
  includes sub-floor intents filtered by the collapse block - fixtures should
  include noise intents below `compoundIntentConfidenceFloor`.
- If clause-splitting infrastructure does not exist, restrict the detector to
  the two-intent case and split on "then"/", and " - do NOT build a general
  NLP clause parser.
- The pair-session path (strategic pair manager) is NOT touched: genuine
  compound work still routes there.
