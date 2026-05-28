# Dispatcher Model Reassignment Implementation Plan

Date: 2026-05-27
Status: Ready for Implementation

## Overview

Implement natural language model reassignment in the dispatcher, allowing users to override agent model assignments with instructions like "use GLM models for coding" or "research with local models, synthesize with glm-4.7".

## Design Reference

- Design spec: `docs/superpowers/specs/2026-05-27-dispatcher-model-reassignment-design.md`

## Implementation Phases

### Sprint 1: Parser Foundation

**Goal**: Create the model reassignment parser with pattern matching and ambiguity detection.

**Tasks**:

1. **Create `internal/agent/model_parser.go`**
   - Add `ModelReassignmentParser` struct
   - Add `ParseResult` struct
   - Implement regex patterns for common phrasings
   - Implement `Parse()` method
   - Add `scopeKeywords` map (scope words → intent types)
   - Add `modelAliases` map (user-friendly names → model refs)
   - Add `providerNames` map (provider keywords → provider IDs)

2. **Add `ModelReassignmentDirective` struct to `internal/agent/dispatcher.go`**
   - Add fields: `Instruction`, `TargetScope`, `TargetIntent`, `ModelReferences`, `ResolvedModels`, `ClarificationNeeded`, `ClarificationQuestions`

3. **Create `internal/agent/model_parser_test.go`**
   - Test each regex pattern
   - Test scope keyword resolution
   - Test model alias resolution
   - Test ambiguity detection

**Deliverables**:
- [ ] `internal/agent/model_parser.go` created
- [ ] `ModelReassignmentDirective` added to dispatcher.go
- [ ] `internal/agent/model_parser_test.go` created with passing tests

---

### Sprint 2: Dispatcher Integration

**Goal**: Wire the parser into the dispatcher's classification pipeline.

**Tasks**:

1. **Add parser to `Dispatcher` struct**
   - Add `modelParser *ModelReassignmentParser` field
   - Initialize in `NewDispatcher()`

2. **Modify `DispatchResult` struct**
   - Add `ModelDirective *ModelReassignmentDirective`
   - Add `ClarificationReply string`
   - Add `ClarificationNeeded bool`

3. **Modify `ClassifyAndRoute()` method**
   - Call `modelParser.Parse(input)` at start
   - If `ClarificationNeeded`, return early with clarification question
   - If clear, resolve model references via `resolver.ResolveRef()`
   - Resolve scope to intent type

4. **Add `buildClarificationQuestion()` method**
   - Handle "no models parsed" case
   - Handle "no scope parsed" case
   - Handle "unknown model" case
   - Handle "multiple models match" case

5. **Add tests for dispatcher integration**
   - Test clarification flow
   - Test successful parsing flow
   - Test model resolution

**Deliverables**:
- [ ] `modelParser` field added to `Dispatcher`
- [ ] `DispatchResult` extended with model directive fields
- [ ] `ClassifyAndRoute()` modified to handle model directives
- [ ] `buildClarificationQuestion()` implemented
- [ ] Integration tests passing

---

### Sprint 3: Task Integration

**Goal**: Thread model overrides through task decomposition.

**Tasks**:

1. **Modify `TaskStep` struct in `internal/task/task.go`**
   - Add `ModelOverride string` field
   - Add JSON tag

2. **Modify task decomposition in dispatcher**
   - If `ModelDirective` exists, match scope to steps
   - For each step, if intent matches `TargetIntent`, set `ModelOverride`
   - Handle multi-model lists (use first available, or create alias)

3. **Add scope matching logic**
   - Match `TargetScope` keyword to step description
   - Match `TargetIntent` to step's assigned agent intent
   - Handle "all" or "entire task" scope

4. **Add tests**
   - Test model override attachment to steps
   - Test scope matching
   - Test multi-step task with partial overrides

**Deliverables**:
- [ ] `TaskStep.ModelOverride` field added
- [ ] Task decomposition modified to attach overrides
- [ ] Scope matching logic implemented
- [ ] Task integration tests passing

---

### Sprint 4: AgentLoop Integration

**Goal**: Apply model overrides during agent execution.

**Tasks**:

1. **Modify `reasoningCycle()` in `internal/agent/loop.go`**
   - Check `l.currentTaskStep.ModelOverride` before LLM call
   - If set, resolve via `l.resolver.ResolveRef()`
   - Call `l.llmClient.SwitchModel()` if resolution succeeds
   - Add log message for model switch

2. **Handle model switch failures gracefully**
   - If `ResolveRef()` fails, log warning and continue with default
   - If `SwitchModel()` fails, log warning and continue with default
   - Standard agent failover applies (no special handling)

3. **Add tests**
   - Test model switch on step with override
   - Test no switch on step without override
   - Test graceful handling of resolution failure

**Deliverables**:
- [ ] `reasoningCycle()` modified to check and apply `ModelOverride`
- [ ] Graceful failure handling
- [ ] AgentLoop tests passing

---

### Sprint 5: Documentation and CLI Examples

**Goal**: Document the feature and add CLI examples.

**Tasks**:

1. **Update `docs/concepts/multi-agent.md`**
   - Add section on model reassignment
   - Include usage examples
   - Document clarification dialog behavior

2. **Update `CLAUDE.md`**
   - Add model reassignment to feature list
   - Include CLI examples

3. **Add CLI examples**
   - Single instruction with override
   - Multi-step task with partial overrides
   - Clarification dialog examples

4. **Update `docs/reference/cli.md`** (if needed)
   - Document any new flags or options

**Deliverables**:
- [ ] `docs/concepts/multi-agent.md` updated
- [ ] `CLAUDE.md` updated
- [ ] CLI examples added

---

## Testing Strategy

### Unit Tests

```go
// model_parser_test.go
func TestModelReassignmentParser_Parse(t *testing.T) {
    tests := []struct {
        name        string
        input       string
        wantFound   bool
        wantScope   string
        wantModels  []string
        wantAmbig   bool
    }{
        {"use X for Y", "use GLM for coding", true, "coding", []string{"GLM"}, false},
        {"X models for Y", "glm models for synthesis", true, "synthesis", []string{"glm"}, false},
        {"ambiguous model", "use local models", true, "", []string{"local"}, true},
        {"ambiguous scope", "use glm models", true, "", []string{"glm"}, true},
        {"no match", "do this task", false, "", nil, false},
    }
    // ...
}
```

### Integration Tests

```go
// dispatcher_integration_test.go
func TestDispatcher_ModelReassignment(t *testing.T) {
    // Test clarification flow
    result, err := disp.ClassifyAndRoute(ctx, "use GLM models", "session-1")
    if result.ClarificationNeeded {
        // Verify clarification question is sensible
    }

    // Test successful parse and routing
    result, err = disp.ClassifyAndRoute(ctx, "use glm-4.7 for coding", "session-2")
    if result.ModelDirective != nil {
        // Verify model directive is populated
    }
}
```

### AgentLoop Tests

```go
// loop_model_override_test.go
func TestAgentLoop_ModelOverride(t *testing.T) {
    // Test model switch on step with override
    step := &task.TaskStep{ModelOverride: "zai/glm-4.7"}
    loop.currentTaskStep = step

    // Verify SwitchModel() is called
}
```

---

## Rollout Plan

1. **Phase 1**: Parser + Dispatcher (Sprints 1-2)
   - Test with CLI interactive mode
   - Verify clarification dialog works

2. **Phase 2**: Task + AgentLoop (Sprints 3-4)
   - Test with multi-step tasks
   - Verify model switching during execution

3. **Phase 3**: Documentation + Polish (Sprint 5)
   - Add examples
   - User testing

---

## Success Metrics

- Parser correctly identifies >90% of model reassignment patterns
- Clarification dialogs are helpful and actionable
- Model overrides correctly applied to matching steps
- No regressions in standard agent execution
- Clean test coverage (>80% for new code)

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Regex patterns miss edge cases | Start with common patterns, expand based on user feedback |
| Model override conflicts with skill requirements | Skill `ResolveForSkill()` takes precedence; override applies to base agent model |
| Multiple models in list causes confusion | Use first available model; log others as fallback |
| Clarification dialogs are annoying | Make them concise; allow "just use default" option |

---

## Future Enhancements (Out of Scope)

- Persistent model preferences per user
- Model alias configuration in `models.json5`
- "Lock model" option to prevent automatic failover
- Model cost tracking for override decisions
- Per-agent model preferences
