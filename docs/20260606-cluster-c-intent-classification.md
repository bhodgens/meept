# Cluster C: Intent Analysis and Enhanced Classification

## Goal
Implement IntentGate-style true-intent analysis, integrate Prometheus-style planning into the planner agent, and improve dispatcher classification accuracy.

## Background
Meept's current `LLMClassifier` classifies directly into intent types with keyword fallback. OMO's IntentGate explicitly analyzes "what the user actually wants" before classification. oh-my-pi's planning is role-based (default/smol/slow/plan/commit).

## Feature Checklist

### 1. IntentGate-Style Pre-Classification
- Add an explicit "true intent analysis" step before classification
- For ambiguous requests, ask clarifying questions (not yet classified)
- Pattern: research? implementation? investigation? fix?
- Store analysis result as metadata for downstream routing
- OMO Light only recognizes `ultrawork`/`ulw` keyword; Ultimate has full analysis

### 2. Integrate Prometheus-Style Planning
- OMO's Prometheus agent interviews the user before coding
- Should be integrated into Meept's `planner` agent
- On `IntentPlan` or complex `IntentCode`, interview the user:
  - Identify scope and ambiguities
  - Build verified plan before touching code
  - Present plan for approval before execution

### 3. Clarification Dialog in Dispatcher
- When confidence is below threshold for the top intent, ask:
  - "You're asking about X — did you mean to implement this or research it?"
- Use `ask` tool for structured follow-up
- Only route after clarification received

## Implementation Plan

### Phase 1: Intent Analysis Layer
1. Create `internal/agent/intent_analyzer.go`
2. `AnalyzeTrueIntent(input string) -> IntentAnalysis{ goal, ambiguity, scope, category }`
3. Use lightweight model for analysis (can reuse LLMClassifier)
4. Add to dispatcher flow: classify -> analyze -> maybe ask -> route

### Phase 2: Planner Agent Interview Mode
1. In `planner` agent's decompose step, add interview phase
2. If input contains ambiguity markers ("maybe", "or", "?"), ask questions
3. Store responses as `PlanningContext` attached to plan steps
4. Present final plan to user for approval before scheduling

### Phase 3: Clarification Integration
1. Add `ClarificationNeeded` intent category
2. Dispatcher emits `ask` tool when analysis shows high ambiguity
3. Resume classification after user response

## Files to Modify / Create
- `internal/agent/intent_analyzer.go` (new) — True intent analysis
- `internal/agent/llm_classifier.go` — Integration of analysis
- `internal/agent/dispatcher.go` — Ask-before-route for ambiguous
- `internal/agent/planner.go` — Interview mode before decomposition
- `internal/plan/types.go` — PlanningContext field

## Success Criteria
- [x] Dispatcher asks clarifying questions for ambiguous requests (e.g., "use GLM" -> "Which GLM model?")
- [x] Planner agent interviews user before complex plans
- [x] Plans are presented for approval before agent scheduling
- [x] Classification accuracy improves (fewer misrouted intents)
