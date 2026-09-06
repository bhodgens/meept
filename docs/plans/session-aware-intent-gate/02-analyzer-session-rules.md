# Analyzer Session Rules - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Pass the session digest into the intent analyzer and add context-conditioned ambiguity rules; update both dispatcher call sites.
- **Dependencies:** 01 (SessionContextDigest + buildSessionContextDigest)
- **Estimated Context:** 30K
- **Concurrency Group:** B

## Goal

AnalyzeTrueIntent currently sends {system prompt, raw input} — blind to
the session. With leaf 01's digest available, this leaf (a) extends the
analyzer signature to accept the digest, (b) appends a compact
"[Recent session activity]" block to the user message when non-empty,
(c) adds two system-prompt rules for context-conditioned ambiguity, and
(d) updates both dispatcher call sites to build and pass the digest.
Contextless turns produce a byte-identical prompt to today's.

## Context

AnalyzeTrueIntent (internal/agent/intent_analyzer.go:106) builds
messages := [{system, systemPrompt}, {user, input}] and calls
chatWithFailover. The systemPrompt string (111-128) carries the JSON
schema + rules. Dispatcher call sites: dispatcher.go:665 (fresh input,
followed by the ambiguity gate at 667-669) and :1266 (post-clarification
combined input, gate at 1268). buildSessionContextDigest exists from
leaf 01 (internal/agent/session_digest.go).

Key files to understand before implementing:
- internal/agent/intent_analyzer.go - AnalyzeTrueIntent (106-146), systemPrompt (111-128)
- internal/agent/dispatcher.go:660-670 - call site 1 (buildMemoryContext at 661 precedes; digest can be built alongside)
- internal/agent/dispatcher.go:1264-1273 - call site 2 (post-clarification; buildMemoryContext at 1279 follows — build digest BEFORE the analysis call there)
- internal/agent/session_digest.go - leaf 01's digest

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/intent_analyzer.go
// func (ia *IntentAnalyzer) AnalyzeTrueIntent(ctx context.Context,
//   input string, sessionContext *SessionContextDigest)
//   (*TrueIntentAnalysis, error)
// Behavior:
//   - sessionContext == nil OR IsEmpty() → messages EXACTLY as today
//     (system prompt identical, user message == input alone)
//   - non-empty digest → user message becomes:
//       <input>
//
//       [Recent session activity]
//       Last task: <LastTaskName> (state: <LastTaskState>, agent: <LastTaskAgent>)
//       Result summary: <LastResultSummary>
//     (omit the "Last task:" line's state/agent segments when empty;
//      omit "Result summary:" line when empty)
//   - system prompt gains two rules (appended to the Rules section):
//       - "Recent session activity may be provided with the input. Use it
//         to resolve pronouns and references ('the change', 'the file',
//         'it') against what was just done."
//       - "If the input is a short follow-up question about the recent
//         activity and the activity makes the referent clear, set
//         ambiguity LOW and proceed — do not ask the user to re-specify."
//     (the rules are present in EVERY call — harmless without context)
// internal/agent/dispatcher.go
//   - call site 1 (~661): digest := d.buildSessionContextDigest(sessionID)
//     BEFORE the AnalyzeTrueIntent call; pass it.
//   - call site 2 (~1266): same, digest built before the analysis call.
// Owner: 02. Consumers: 03 (e2e A5 green).
```

### What This Leaf Consumes

```
// SessionContextDigest + IsEmpty from leaf 01
// d.buildSessionContextDigest from leaf 01
```

## Tasks

### Task 1: analyzer signature + prompt blocks

**Objective:** Digest-aware analysis with contextless byte-compatibility.

**Files:**
- Modify: `internal/agent/intent_analyzer.go` (signature 106, prompt 111-128, messages 130-133)
- Test: `internal/agent/intent_analyzer_test.go`

**Step 1: Write failing tests** — using the existing fake-client pattern
in intent_analyzer_test.go (capture the messages passed to the client):
(a) nil digest → user message == input, no "[Recent session activity]";
(b) populated digest → user message contains input, "[Recent session
activity]", "Last task:", "Result summary:"; (c) partially-filled digest
(empty summary) → no "Result summary:" line; (d) system prompt contains
the two new rules in ALL cases.

**Step 2: verify failure** (signature change → compile error is the red
phase for call sites; analyzer-internal tests fail on missing block) →
implement → verify pass.

**Step 3: run** `go test -p 2 ./internal/agent/ -run TestIntentAnalyzer -count=1 -v`

### Task 2: dispatcher call sites

**Objective:** Both analysis calls build and pass the digest.

**Files:**
- Modify: `internal/agent/dispatcher.go:665` and `:1266`
- Test: `internal/agent/dispatcher_test.go` (or existing dispatcher test
  file covering ClassifyAndRoute — search for it; if the intent-analyzer
  interaction there is fake-based, extend the fake to assert the digest
  argument is non-nil when a task exists for the session)

**Step 1: Write failing test** — dispatcher with taskStore containing a
completed task linked to the session + fake/real analyzer capturing the
sessionContext argument; call ClassifyAndRoute with "did the change get
made?"; assert sessionContext != nil && !IsEmpty().

**Step 2: verify failure → implement** (one line per site:
`digest := d.buildSessionContextDigest(sessionID)` before each
AnalyzeTrueIntent, pass digest) → **verify pass.**

**Step 3: package** — `go test -p 2 ./internal/agent/ -count=1` green.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Contextless prompt byte-identical (test-proven)
- [ ] Threshold 0.6 unchanged; no keyword lists; no config added

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Signature extended; both dispatcher call sites pass digest
- [ ] Digest built BEFORE the analysis call at both sites
- [ ] Contextless behavior byte-identical (test-proven)
- [ ] New rules present in every system prompt
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- intent_analyzer_test.go has a fake LLM client pattern — reuse it; do
  not invent a new mock style.
- If dispatcher tests have no analyzer seam, test the digest-building
  (leaf 01 tests already cover it) and assert here only that both call
  sites compile passing a non-nil digest built from the real method —
  integration proof is leaf 03's e2e. Document as deviation.
- Keep the [Recent session activity] block EXACTLY in the contract's
  format — leaf 03's e2e and the review both pattern-match it.
