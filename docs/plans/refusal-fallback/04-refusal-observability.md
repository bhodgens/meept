# Refusal Fallback Observability - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** WS classification guard, surface parity (TUI/GUI status), ledger identity pin.
- **Dependencies:** 03-loop-refusal-branch.md (the branch that publishes the event)
- **Estimated Context:** 40K
- **Concurrency Group:** B (after 03)

## Goal

The fallback must be visible, never silent (the Fable-5 lesson). The bus
event from leaf 03 already flows over WS as agent_progress via the existing
`agent.model_escalated` prefix classification. This leaf pins that
classification with a test, verifies the step result names the model that
served (honest-reply invariant), and confirms the metrics ledger records
the fallback model — no new topic, no schema change.

## Context

WS classification lives in `internal/comm/http/server.go`
(transformBusEventToWS): `strings.HasPrefix(topic, "agent.model_escalated")`
=> `agent_progress`. The Flutter client creates visible bubbles only for
`chat_message` — misclassification shows blank bubbles. The metrics ledger
(`~/.meept/metrics.db` llm_calls, provider/model_id columns) already names
the actually-serving model per the resolved-model identity invariant; the
refusal retry reuses the same chatWithFailover path, so identity should be
correct by construction — this leaf proves it.

Key files to understand before implementing:
- internal/comm/http/server.go - transformBusEventToWS prefix buckets
- internal/agent/verification_escalation.go:37 - the reused topic
- internal/llm/client.go:973-995 - resolved-model identity comment block

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/comm/http/ws_classification_refusal_test.go
package http // the server package's actual test package name — check an
// existing server test via search_files and match it.

// TestWS_ClassifiesRefusalFallbackEvent: a bus message on
// "agent.model_escalated" with payload reason "refusal_fallback"
// transforms to WS type "agent_progress" — regression pin for the
// existing prefix rule (no production change expected; if it fails the
// prefix rule does not exist as documented and THAT is the finding).

// File: internal/agent/loop_refusal_observability_test.go
// TestRefusalFallback_StepResultNamesFallbackModel: after a successful
// fallback retry, the stored/returned step result mentions the fallback
// model id (honest-reply invariant: machine output never masquerades as
// the originally-selected model's answer without disclosure).
// If leaf 03's retry already includes the model in the result text, this
// test pins it; if not, add the sentence in the smallest possible way:
// append to the step result: "[served by <fallback model> after refusal]".
```

### What This Leaf Consumes

Leaf 03's handleRefusal + event publisher. No new production types.

## Tasks

### Task 1: WS classification regression pin

**Objective:** Prove refusal-fallback events reach clients as agent_progress.

**Files:**
- Test: `internal/comm/http/ws_classification_refusal_test.go` (name/match
  the existing server test package)

**Step 1: Write the test** — locate transformBusEventToWS via search_files,
mirror an existing classification test's setup (search_files for
"agent_progress" in internal/comm/http/*_test.go).

**Step 2: Run.** Expected: PASS with no production change. If FAIL, the
classification prefix needs the refusal reason added — fix server.go
minimally and note the deviation.

**Step 3: Full package** — `go test -p 2 ./internal/comm/http/ -short`

### Task 2: Honest step-result disclosure

**Objective:** The user's reply discloses the fallback served it.

**Files:**
- Test + (only if needed) Modify: `internal/agent/loop_refusal.go` or the
  result-assembly site leaf 03 used.

**Step 1: Write failing test** per contract (fallback success => result
text contains the fallback model id).

**Step 2: Run to verify failure.**

**Step 3: Implement** the one-sentence disclosure append (smallest change;
do not reformat existing result text).

**Step 4: Run to verify pass** + `go test -p 2 ./internal/agent/ -short`

### Task 3: Ledger identity verification (test only)

**Objective:** Prove the refusal retry's llm_calls row names the fallback
provider/model.

**Files:**
- Test: extend the leaf-03 e2e-style test OR the client-level test from
  leaf 01's Task 4 — assert the usage/metrics recording path receives the
  fallback model id on the second call. Use the existing metrics-store
  test double (search_files for llm_calls in internal/metrics tests).

**Step 1-3:** Write, run, keep as regression pin. No production change
expected (identity invariant). A failure here is a REAL finding: report it
as a deviation, do not hack the recorder.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] No new bus topic introduced (grep agent.refusal over internal/ => zero)
- [ ] No metrics schema change
- [ ] Disclosure sentence appended, existing result text untouched
- [ ] gofmt clean; go vet passes on touched packages

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] WS pin test present and passing
- [ ] Honest-reply disclosure test present and passing
- [ ] Ledger identity test present and passing
- [ ] Zero new topics; zero schema changes
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- TUI/GUI feature parity (a visible "refused -> fell back" status line) is
  OUT of scope for this tree: the agent_progress event already reaches both
  clients' progress channels. Record as follow-up if the user wants a
  bespoke status element.
