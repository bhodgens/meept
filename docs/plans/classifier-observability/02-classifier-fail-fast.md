# Classifier Fail-Fast - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Add classifier_fail_fast (default false): when true, the intent analyzer returns the primary classifier's error immediately instead of rotating to weaker alias members.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A

## Goal

The analyzer's rotation (zai → LFM2.5-8B → LFM2.5-1.2B → glm-4.5-air) keeps
turns working while hiding that a 1.2B model is now judging ambiguity —
yielding verdicts like 0.8 on "create a file named hello.txt...". For
testing and iteration, engineers need a mode where the primary's failure
is HONEST: the turn fails with the primary error, no silent downgrade.
This leaf adds that mode, default-off.

## Context

Rotation lives in IntentAnalyzer.chatWithFailover
(internal/agent/intent_analyzer.go ~206-243): attempt() → on error, when
ia.resolver != nil && ia.aliasName != "", RecordAliasFailure +
ResolveForAlias + Reconfigure + one retry. The analyzer is constructed in
NewDispatcher via newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
ModelConfig, Resolver, AliasName}, ...) (dispatcher.go ~398). Config
surface: internal/config/schema.go OrchestratorConfig (2236-2247,
defaults 2884) → threaded at components.go NewDispatcher (~2390
DispatcherConfig block).

Key files to understand before implementing:
- internal/agent/intent_analyzer.go:61-75 IntentAnalyzerConfig, 206-243 chatWithFailover
- internal/agent/dispatcher.go:295-330 DispatcherConfig, ~398 analyzer construction
- internal/config/schema.go:2236-2247 OrchestratorConfig, ~2884 defaults
- internal/daemon/components.go:2390+ NewDispatcher wiring

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/config/schema.go — OrchestratorConfig gains:
//   ClassifierFailFast bool `json:"classifier_fail_fast" toml:"classifier_fail_fast"`
//   // comment: when true, the intent analyzer fails the turn with the
//   // primary classifier's error instead of rotating to weaker alias
//   // members. Default false (production keeps rotation). For testing.
//   Defaults block adds ClassifierFailFast: false.
// internal/agent/intent_analyzer.go — IntentAnalyzerConfig gains FailFast
//   bool; IntentAnalyzer stores it; chatWithFailover: on primary failure
//   AND FailFast → Warn "classifier fail-fast: no rotation (configured)"
//   + return the primary error immediately (skip RecordAliasFailure/
//   ResolveForAlias/Reconfigure/retry entirely).
// internal/agent/dispatcher.go — DispatcherConfig gains ClassifierFailFast
//   bool; NewDispatcher passes cfg.ClassifierFailFast into
//   IntentAnalyzerConfig.FailFast.
// internal/daemon/components.go — DispatcherConfig literal gains
//   ClassifierFailFast: cfg.Orchestrator.ClassifierFailFast.
// Owner: 02. Consumers: testing workflow; independent of 01/03.
```

### What This Leaf Consumes

```
// existing rotation machinery (unmodified when FailFast=false)
```

## Tasks

### Task 1: IntentAnalyzer FailFast

**Objective:** Rotation skipped when configured.

**Files:**
- Modify: `internal/agent/intent_analyzer.go`
- Test: `internal/agent/intent_analyzer_test.go`

**Step 1: Write failing tests** — with a capture server failing the first
attempt (500) and a resolver configured:
(a) FailFast=false (default): rotation occurs (second attempt hits the
rotate-target server; assert 2 requests);
(b) FailFast=true: exactly 1 request, error returned names the primary
failure, and NO RecordAliasFailure side effect observable (or assert via
exposed stats if available).
Follow the httptest capture pattern from classifier_failover_test.go.

**Step 2: verify failure → implement (config field + early-return) →
verify pass.**
Run: `go test -p 2 ./internal/agent/ -run 'TestIntentAnalyzer_FailFast' -count=1 -v`

### Task 2: config + dispatcher threading

**Objective:** Config reaches the analyzer.

**Files:**
- Modify: `internal/config/schema.go` (OrchestratorConfig + defaults)
- Modify: `internal/agent/dispatcher.go` (DispatcherConfig + pass-through at ~398)
- Modify: `internal/daemon/components.go` (DispatcherConfig literal)
- Test: `internal/config/schema_test.go` (default false; json5 key round-trips)

**Step 1: Write failing test** — schema default block includes
ClassifierFailFast=false; json5 with "classifier_fail_fast": true
parses true.

**Step 2: verify → implement → verify.**
Run: `go test -p 2 ./internal/config/ ./internal/agent/ -count=1`

### Task 3: package + build green

`go build ./...` and both packages' suites green.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Default OFF: zero behavior change without the config key
- [ ] Rotation logic untouched when FailFast=false

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] FailFast=false: byte-identical behavior to today
- [ ] FailFast=true: single attempt, primary error, no alias-failure recording
- [ ] Config threaded schema → dispatcher → analyzer
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Resolver semantics untouched — RecordAliasFailure still happens on
  FailFast=false exactly as today.
- The user-facing config comment must state: default false; exists to
  make classifier failures honest during testing/iteration.
