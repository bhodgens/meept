# Dispatcher Model Reassignment Design

Date: 2026-05-27
Status: Draft

## Problem

Users cannot override agent model assignments from natural language instructions. When working on tasks requiring specific model capabilities (e.g., "use GLM models for synthesis" or "research with local models, code with GLM-4.7"), the system uses default model resolution without considering user preferences.

The dispatcher should parse model reassignment instructions from free-form text, clarify ambiguities via dialog, and route tasks with user-specified model overrides.

## Design

### Architecture

```
User Input: "Research this, then use glm-4.7 for the synthesis"
                            │
                            ▼
┌────────────────────────────────────────────────────────────┐
│  1. Dispatcher.ClassifyAndRoute()                          │
│  - ModelReassignmentParser.Parse(input)                    │
│  - If ambiguous: return ClarificationReply                 │
│  - If clear: populate DispatchResult.ModelDirective        │
└────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────────┐
│  2. Task Decomposition (if multi-step)                     │
│  - Match directive scope to intent types                   │
│  - Attach ModelOverride to matching TaskSteps              │
│  - Example: "synthesis" → IntentPlan → TaskStep.ModelOverride│
└────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────────┐
│  3. AgentLoop.ReasoningCycle()                             │
│  - Read current TaskStep.ModelOverride                     │
│  - If set: resolver.ResolveRef(model_override)             │
│  - SwitchModel() before LLM call                           │
│  - On failure: standard agent failover (no special handling)│
└────────────────────────────────────────────────────────────┘
```

### Key Principles

1. **Per-task scope**: Model overrides apply to specific task steps, not entire conversations
2. **Intent-based matching**: Scope keywords map to intent types (e.g., "synthesis" → IntentPlan, "coding" → IntentCode)
3. **Natural language parsing**: Free-form text with clarification dialog for ambiguity
4. **Standard failure handling**: Model failures use existing agent failover logic (retry, fallback, error)

---

## Components

### ModelReassignmentDirective

Captures parsed model reassignment instructions.

```go
// internal/agent/dispatcher.go

// ModelReassignmentDirective captures a user's model reassignment instruction.
type ModelReassignmentDirective struct {
    // Raw user instruction text (e.g., "use GLM models for coding")
    Instruction string `json:"instruction"`

    // TargetScope - which intent type this applies to
    // Examples: "synthesis"→IntentPlan, "coding"→IntentCode, "research"→IntentResearch
    TargetScope string `json:"target_scope,omitempty"`

    // TargetIntent - resolved intent type from scope keyword
    TargetIntent *IntentType `json:"target_intent,omitempty"`

    // ModelReferences - one or more model specs from user input
    // Can be: "zai/glm-4.7", "glm-*", "provider:zai", "opus"
    ModelReferences []string `json:"model_references"`

    // ResolvedModels - after resolver processes references
    ResolvedModels []*llm.ModelConfig `json:"resolved_models,omitempty"`

    // ClarificationNeeded - set true if instruction is ambiguous
    ClarificationNeeded bool `json:"clarification_needed,omitempty"`

    // ClarificationQuestions - questions to ask user if ambiguous
    ClarificationQuestions []string `json:"clarification_questions,omitempty"`
}
```

### ModelReassignmentParser

Parses natural language model reassignment instructions.

```go
// internal/agent/model_parser.go (NEW FILE)

// ModelReassignmentParser parses natural language model reassignment instructions.
type ModelReassignmentParser struct {
    patterns       []*regexp.Regexp
    scopeKeywords  map[string]IntentType
    modelAliases   map[string]string
    providerNames  map[string]string  // e.g., "glm" → "zai"
}

// ParseResult is the result of parsing a model reassignment instruction.
type ParseResult struct {
    Found       bool
    Directive   *ModelReassignmentDirective
    Ambiguities []string
}

// Parse parses input for model reassignment instructions.
func (p *ModelReassignmentParser) Parse(input string) *ParseResult

// Example patterns:
var modelReassignmentPatterns = []string{
    // "use X for Y"
    `(?i)use\s+(?P<models>[\w\s/\-\.]+)\s+for\s+(?P<scope>[\w\s\-]+)`,

    // "X models only for Y"
    `(?i)(?P<models>[\w\s/\-\.]+)\s+models?\s+(?:only\s+)?for\s+(?P<scope>[\w\s\-]+)`,

    // "do this with X"
    `(?i)do\s+(?:this|that)\s+with\s+(?P<models>[\w\s/\-\.]+)`,

    // "synthesize using X" / "analyze with X" / "code via X"
    `(?i)(?P<action>synthesiz|analys|research|code|plan|debug)\s+(?:using|with|via)\s+(?P<models>[\w\s/\-\.]+)`,

    // "I want X to handle Y"
    `(?i)want\s+(?P<models>[\w\s/\-\.]+)\s+to\s+handle\s+(?P<scope>[\w\s\-]+)`,
}

// Scope keyword mappings (maps to intent types)
var scopeKeywords = map[string]IntentType{
    "synthesis":  IntentPlan,
    "synthesize": IntentPlan,
    "planning":   IntentPlan,
    "plan":       IntentPlan,

    "coding":     IntentCode,
    "code":       IntentCode,
    "programming": IntentCode,
    "implementation": IntentCode,

    "research":   IntentResearch,
    "analysis":   IntentAnalysis,
    "analyze":    IntentAnalysis,

    "debugging":  IntentDebug,
    "debug":      IntentDebug,
    "troubleshooting": IntentDebug,
}

// Model name aliases (user-friendly names → actual model refs)
var modelAliases = map[string]string{
    "opus":      "anthropic/claude-3-opus",
    "sonnet":    "anthropic/claude-3-sonnet",
    "haiku":     "anthropic/claude-3-haiku",
    "gpt-4":     "openai/gpt-4",
    "gpt-4o":    "openai/gpt-4o",
    "glm":       "zai/glm-4.7",
    "glm-4.7":   "zai/glm-4.7",
    "glm-4.5":   "zai/glm-4.5-air",
    "qwen":      "ollama/qwen2.5-coder",
    "llama":     "ollama/llama3.2",
}

// Provider name mappings (e.g., "glm" → "zai")
var providerNames = map[string]string{
    "glm":    "zai",
    "qwen":   "ollama",
    "llama":  "ollama",
    "claude": "anthropic",
    "gpt":    "openai",
}
```

### Ambiguity Detection

The parser detects these ambiguities and triggers clarification:

| Ambiguity Type | Example | Clarification Question |
|----------------|---------|----------------------|
| Multiple models match "glm" | "use GLM models" | "Which GLM model: `glm-4.7` (most capable) or `glm-4.5-air` (faster)?" |
| Scope ambiguity | "use local models for this" | "Should local models handle the entire task, or just specific parts (research, coding, synthesis)?" |
| Provider ambiguity | "use opus" | "Did you mean Claude Opus for analysis, coding, or the entire task?" |
| No matching models | "use gpt-3.5" | "Model 'gpt-3.5' is not configured. Available models: [list]" |

---

### Dispatcher Integration

```go
// internal/agent/dispatcher.go

type DispatchResult struct {
    // Existing fields...
    Task          *task.Task `json:"task,omitempty"`
    AgentID       string     `json:"agent_id"`
    Intent        *Intent    `json:"intent"`

    // NEW: Model reassignment if user specified one
    ModelDirective *ModelReassignmentDirective `json:"model_directive,omitempty"`

    // NEW: Clarification response if needed
    ClarificationReply string `json:"clarification_reply,omitempty"`
}

// ClassifyAndRoute with model reassignment parsing
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input, sessionID string) (*DispatchResult, error) {
    // Step 1: Parse model reassignment instruction
    parseResult := d.modelParser.Parse(input)

    // Step 2: If clarification needed, return early with question
    if parseResult.Found && parseResult.Directive.ClarificationNeeded {
        return &DispatchResult{
            ModelDirective:       parseResult.Directive,
            ClarificationReply:   d.buildClarificationQuestion(parseResult.Directive),
            ClarificationNeeded:  true,
        }, nil
    }

    // Step 3: Normal classification and routing
    intent := d.classifyIntent(ctx, input, memCtx)

    // Step 4: If model directive exists, resolve models
    if parseResult.Found && parseResult.Directive != nil {
        for _, ref := range parseResult.Directive.ModelReferences {
            mc := d.resolver.ResolveRef(ref)
            if mc != nil {
                parseResult.Directive.ResolvedModels = append(parseResult.Directive.ResolvedModels, mc)
            }
        }

        // Resolve scope keyword to intent type
        if parseResult.Directive.TargetScope != "" {
            if intentType, ok := d.modelParser.ResolveScope(parseResult.Directive.TargetScope); ok {
                parseResult.Directive.TargetIntent = &intentType
            }
        }
    }

    // Step 5: Decompose into tasks if needed, attach model directive to relevant steps
    // ... existing task decomposition logic ...
    // If multi-step task, match directive scope to step intents

    return result, nil
}

// buildClarificationQuestion generates a clarification dialog for ambiguous directives.
func (d *Dispatcher) buildClarificationQuestion(directive *ModelReassignmentDirective) string {
    // Check for specific ambiguity types
    if len(directive.ModelReferences) == 0 {
        // No models parsed - list available options
        return d.buildModelListQuestion(directive.TargetScope)
    }

    if directive.TargetScope == "" {
        // No scope parsed - ask what the models should handle
        return d.buildScopeQuestion(directive.ModelReferences)
    }

    // Generic fallback
    return fmt.Sprintf(
        "I want to make sure I use the right model. You mentioned '%s' - could you clarify which model and what it should handle?",
        directive.Instruction,
    )
}
```

---

### Task-Level Model Override

```go
// internal/task/task.go

type TaskStep struct {
    ID          string          `json:"id"`
    Description string          `json:"description"`
    Status      StepStatus      `json:"status"`
    AgentID     string          `json:"agent_id,omitempty"`

    // NEW: Model override for this specific step
    ModelOverride string `json:"model_override,omitempty"`

    // Execution result
    Output string `json:"output,omitempty"`
}
```

---

### AgentLoop Integration

```go
// internal/agent/loop.go

func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
    // Check if current task step has model override
    if l.currentTaskStep != nil && l.currentTaskStep.ModelOverride != "" {
        mc, err := l.resolver.ResolveRef(l.currentTaskStep.ModelOverride)
        if err == nil && mc != nil {
            // Switch model for this iteration
            if l.llmClient != nil {
                l.llmClient.SwitchModel(mc)
                l.logger.Info("Model overridden for task step",
                    "step", l.currentTaskStep.ID,
                    "model", mc.ModelID,
                )
            }
        }
    }

    // ... rest of existing reasoning cycle ...
    // Standard failure handling applies - no special override needed
}
```

---

## Clarification Dialog Examples

### Example 1: Clear Instruction

**User**: "Do the synthesis with glm-4.7"

**Dispatcher** (no ambiguity - clear scope and model):
> No clarification needed. Proceeds with task decomposition.

### Example 2: Ambiguous Model

**User**: "Use GLM models for coding"

**Dispatcher** (multiple GLM models available):
> "I can use GLM models for coding. Which would you prefer?
> - `glm-4.7` (most capable, 128K context)
> - `glm-4.5-air` (faster, 32K context)
>
> Or press enter for default (glm-4.7)"

### Example 3: Ambiguous Scope

**User**: "Use only local models"

**Dispatcher** (no scope specified):
> "I found several local models available:
> - `lfm-code` (1.2B, optimized for code)
> - `lfm-24b` (largest, general purpose)
> - `lfm-thinking-claude` (reasoning focus)
>
> What should these models handle - research, coding, synthesis, or the entire task?"

### Example 4: Unknown Model

**User**: "Use gpt-3.5 for this"

**Dispatcher** (model not configured):
> "Model 'gpt-3.5' is not in the configured models. Available models:
> - `zai/glm-4.7`, `zai/glm-4.5-air`
> - `local/lfm-code`, `local/lfm-24b`
> - `ollama/llama3.2`, `ollama/qwen2.5-coder`
>
> Which would you like to use?"

---

## Implementation Phases

### Phase 1: Parser Foundation

| Task | Description |
|------|-------------|
| Create `internal/agent/model_parser.go` | Core parsing logic with regex patterns |
| Add `ModelReassignmentDirective` struct | Data structure for parsed directives |
| Add `Parse()` method | Parse input and detect ambiguities |
| Add `scopeKeywords` map | Map scope words to intent types |
| Add `modelAliases` map | User-friendly model name aliases |

### Phase 2: Dispatcher Integration

| Task | Description |
|------|-------------|
| Add `ModelDirective` to `DispatchResult` | Store parsed directive |
| Add `ClarificationReply` to `DispatchResult` | Store clarification questions |
| Wire parser into `ClassifyAndRoute()` | Parse input before classification |
| Add `buildClarificationQuestion()` | Generate clarification dialogs |
| Add resolver integration | Resolve model references to configs |

### Phase 3: Task Integration

| Task | Description |
|------|-------------|
| Add `ModelOverride` field to `TaskStep` | Store per-step model override |
| Modify task decomposition | Attach model directive to matching steps |
| Add scope matching logic | Match directive scope to step intent |

### Phase 4: AgentLoop Integration

| Task | Description |
|------|-------------|
| Read `ModelOverride` in `reasoningCycle()` | Check for step-level override |
| Call `SwitchModel()` if override set | Switch model before LLM call |
| Add logging for model switches | Debug visibility |

### Phase 5: Testing

| Task | Description |
|------|-------------|
| Unit tests for parser patterns | Test regex matching |
| Unit tests for ambiguity detection | Test clarification triggers |
| Integration tests for dispatcher | End-to-end flow |
| Integration tests for AgentLoop | Model switching behavior |

---

## Files to Create

| File | Purpose |
|------|---------|
| `internal/agent/model_parser.go` | Model reassignment parser |
| `internal/agent/model_parser_test.go` | Parser unit tests |

## Files to Modify

| File | Change |
|------|--------|
| `internal/agent/dispatcher.go` | Add `ModelDirective`, `ClarificationReply` to `DispatchResult`; wire parser |
| `internal/agent/loop.go` | Check `ModelOverride` in `reasoningCycle()` |
| `internal/task/task.go` | Add `ModelOverride` field to `TaskStep` |
| `docs/concepts/multi-agent.md` | Document model reassignment feature |
| `CLAUDE.md` | Update with model reassignment usage examples |

---

## Configuration

No configuration changes required. The feature uses:
- Existing `config/models.json5` for model definitions
- Existing resolver for model reference resolution
- Existing agent failover for error handling

Optional future enhancement: Add model alias configuration to `models.json5`:

```json5
{
  // Optional: user-defined model aliases
  "user_aliases": {
    "fast-coder": "zai/glm-4.7",
    "local-researcher": "local/lfm-24b",
    "thinking": "local/lfm-thinking-claude"
  }
}
```

---

## Usage Examples

### CLI Usage

```bash
# Single instruction with model override
meept chat "Research best practices for Go error handling, then use glm-4.7 for synthesis"

# Interactive mode
meept chat
> Use local models for research, glm-4.7 for coding
> [If ambiguous, dispatcher asks clarification questions]
```

### Expected Behavior

1. User instruction parsed for model reassignment
2. If ambiguous, dispatcher returns clarification question
3. User provides clarification (or accepts defaults)
4. Task decomposed with model overrides on matching steps
5. Agent executes with specified models
6. On model failure, standard failover applies (retry → fallback → error)

---

## Success Criteria

1. Parser correctly identifies model reassignment patterns in free-form text
2. Ambiguities trigger clarification dialog
3. Model overrides correctly applied to matching task steps
4. AgentLoop switches models per-step without errors
5. Standard failover handles model failures transparently
