# Provenance Meta - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** master.md
- **Scope:** Surface classification provenance (method + serving model + ambiguity + digest usage) on every classified reply via ChatResponse.Meta.
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

The A5 campaign's core lesson: the system silently degraded from a strong
classifier to a 1.2B local model and nothing visible changed for the user.
This leaf puts classification provenance on the reply itself — method,
serving model, ambiguity score, and whether session context was used — as
structured metadata, so degradation is observable at a glance.

## Context

`Intent` (internal/agent/dispatcher.go:80) already carries `Method`
(classification_method string, set at every classify branch) but not the
serving model. The ambiguity score lives in
`Intent.TrueAnalysis.Ambiguity` when the analyzer ran.
`ChatResponse` (internal/agent/handler.go:182) has Reply/ConversationID/
SessionID/Error — no metadata. `recordClassificationMethod` maintains
per-method stats (dispatcher.go:2407). The serving model name: the
dispatcher's classifier chain resolves via Resolver; the simplest reliable
source is `result.Intent.TrueAnalysis` (analyzer branch) plus the model
id captured at classification time — the analyzer knows it via
`ia.modelConfig` (ProviderID/ModelID after Reconfigure).

Key files to understand before implementing:
- internal/agent/dispatcher.go:80-104 - Intent struct incl. Method
- internal/agent/dispatcher.go:2407 - recordClassificationMethod + stats
- internal/agent/handler.go:182 - ChatResponse struct
- internal/agent/handler.go:655-725 - the ClassifyAndRoute reply path where Meta is populated
- internal/agent/intent_analyzer.go:48-51 - ia.modelConfig (ProviderID/ModelID)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/dispatcher.go
// type Intent gains: Model string `json:"model,omitempty"` — resolved
//   "provider/model" of the classifier/analyzer that served it; empty for
//   non-LLM branches (keyword/heuristic/guard).
//   Set wherever Intent.Method is set for LLM-served branches: the
//   analyzer path sets it from the analyzer's resolved model (expose
//   func (ia *IntentAnalyzer) ResolvedModel() string returning
//   ProviderID+"/"+ModelID from modelConfig, empty when unknown).
// internal/agent/handler.go
// type ChatResponse gains: Meta map[string]string
//   `json:"meta,omitempty"`
// On the classified reply path: when result.Intent != nil, populate
//   Meta = {"classification_method": Intent.Method,
//          "classification_model": Intent.Model,
//          "ambiguity": fmt.Sprintf("%.2f", TrueAnalysis.Ambiguity) — only
//                      when TrueAnalysis != nil,
//          "session_digest_used": "true"/"false" — only when the analyzer
//                      ran (digest arg non-nil)}.
//   Meta omitted entirely when result == nil or Intent == nil (direct
//   mode). Meta is additive metadata; reply text unchanged.
// Owner: 01. Consumers: ops/observability; independent of 02/03.
```

### What This Leaf Consumes

```
// ia.modelConfig → ResolvedModel() accessor (new, intent_analyzer.go)
// TrueAnalysis.Ambiguity (existing)
```

## Tasks

### Task 1: Intent.Model + ResolvedModel accessor

**Objective:** Capture the serving model id on classified intents.

**Files:**
- Modify: `internal/agent/dispatcher.go` (Intent struct + set Model at
  LLM classify branches — search `recordClassificationMethod("llm")` and
  the sibling branches; each site sets `intent.Method`; add
  `intent.Model = d.intentAnalyzer.ResolvedModel()` or the equivalent
  resolved model for that branch)
- Modify: `internal/agent/intent_analyzer.go` (ResolvedModel accessor;
  maintain an ia.servedModel field updated in chatWithFailover after
  Reconfigure — set to nextCfg.ProviderID+"/"+nextCfg.ModelID — and
  initialized from cfg.ModelConfig)
- Test: `internal/agent/dispatcher_test.go` / `intent_analyzer_test.go`

**Step 1: Write failing test** — analyzer-level: build analyzer with a
capture server (httptest pattern from classifier_failover_test.go) whose
response includes valid analysis JSON; call AnalyzeTrueIntent; assert
ResolvedModel() returns the served "provider/model" from the config the
server received (assert against the model config you constructed).

**Step 2: verify failure → implement → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run 'TestIntentAnalyzer_ResolvedModel' -count=1 -v`

### Task 2: ChatResponse.Meta population

**Objective:** Reply carries provenance metadata.

**Files:**
- Modify: `internal/agent/handler.go` (ChatResponse struct + population
  on the classified reply path, before sendResponse ~line 700/866)
- Test: `internal/agent/handler_test.go`

**Step 1: Write failing test** — handler test with a dispatcher fake or
stubbed result carrying Intent{Method:"llm", Model:"agnes/glm", 
TrueAnalysis:&{Ambiguity:0.8}}; assert response.Meta matches; second case
result==nil → Meta nil.

**Step 2: verify failure → implement → verify pass.**
Run: `go test -p 2 ./internal/agent/ -run TestChatResponse_Provenance -count=1 -v`

### Task 3: package + build green

Run `go test -p 2 ./internal/agent/ -count=1` and `go build ./...`.

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep
- [ ] Non-LLM branches leave Model empty (no fake provenance)
- [ ] Meta absent (not empty map) on unclassified replies

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Intent.Model set only where a real model served the classification
- [ ] Meta keys exactly: classification_method / classification_model /
      ambiguity / session_digest_used
- [ ] Reply text untouched; Meta additive
- [ ] No scope creep

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- TrueAnalysis.Ambiguity is only set when the analyzer ran; guard the
  ambiguity key on TrueAnalysis != nil.
- If some classify branches cannot know the model (e.g. capability_matcher
  is local), Model stays "" — that IS the honest provenance.
