# Call Sites and Documentation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../../master.md
- **Scope:** Update all call sites to pass sessionID, update documentation
- **Dependencies:** 01-core-types.md (must be complete first)
- **Estimated Context:** 40K (exploration + generation + iteration + overhead)
- **Concurrency Group:** B

## Goal

Wire the new `callerKey` parameter through all `ResolveForAlias` call sites in the codebase. Agent loops pass their `sessionID` to enable sticky behavior. Classifiers and analyzers pass empty string (no sticky needed). Update documentation to reflect the new configuration options.

## Context

Key files to update:
- `internal/agent/loop.go` — main agent loop, passes `l.sessionID`
- `internal/agent/intent_analyzer.go` — classifier, passes ""
- `internal/agent/llm_classifier.go` — another classifier, passes ""
- `internal/daemon/components.go` — various utility functions, passes ""
- `internal/tools/builtin/media_client.go` — media generation, passes ""
- `docs/configuration/llm.md` — user-facing docs
- `docs/feature-model-aliases.md` — feature docs

## Interface Contracts (From Parent)

### What This Leaf Consumes

```go
// From 01-core-types.md (now implemented)
func (r *Resolver) ResolveForAlias(aliasName string, callerKey string) (*ModelConfig, error)
```

### What This Leaf Exposes

Updated call sites throughout the codebase with correct `callerKey` values.

## Tasks

### Task 1: Update agent loop call sites

**Objective:** Pass `l.sessionID` as caller key in main agent loop

**Files:**
- Modify: `internal/agent/loop.go:3126`
- Modify: `internal/agent/loop.go:4113`

**Step 1: Read current code**

Use `search_files` to find the exact lines:
```bash
grep -n "ResolveForAlias" internal/agent/loop.go
```

**Step 2: Update call sites**

Change line 3126 from:
```go
modelConfig, err := l.resolver.ResolveForAlias(l.modelRef)
```
to:
```go
modelConfig, err := l.resolver.ResolveForAlias(l.modelRef, l.sessionID)
```

Change line 4113 from:
```go
modelConfig, err := l.resolver.ResolveForAlias(l.modelRef)
```
to:
```go
modelConfig, err := l.resolver.ResolveForAlias(l.modelRef, l.sessionID)
```

**Step 3: Verify build**

Run: `go build ./internal/agent/...`
Expected: No errors

### Task 2: Update intent analyzer call sites

**Objective:** Pass empty string (no sticky for classifiers)

**Files:**
- Modify: `internal/agent/intent_analyzer.go:173`
- Modify: `internal/agent/llm_classifier.go:223`
- Modify: `internal/agent/llm_classifier.go:304`

**Step 1: Find call sites**

Use `search_files`:
```bash
grep -n "ResolveForAlias" internal/agent/intent_analyzer.go internal/agent/llm_classifier.go
```

**Step 2: Update call sites**

Change all from:
```go
resolver.ResolveForAlias(aliasName)
```
to:
```go
resolver.ResolveForAlias(aliasName, "")
```

**Step 3: Verify build**

Run: `go build ./internal/agent/...`
Expected: No errors

### Task 3: Update daemon component call sites

**Objective:** Pass empty string for utility functions

**Files:**
- Modify: `internal/daemon/components.go:664`
- Modify: `internal/daemon/components.go:3509`
- Modify: `internal/daemon/components.go:3704`
- Modify: `internal/daemon/components.go:4569`

**Step 1: Find call sites**

Use `search_files`:
```bash
grep -n "ResolveForAlias" internal/daemon/components.go
```

**Step 2: Update call sites**

Change all from:
```go
resolver.ResolveForAlias(...)
```
to:
```go
resolver.ResolveForAlias(..., "")
```

**Step 3: Verify build**

Run: `go build ./internal/daemon/...`
Expected: No errors

### Task 4: Update media client call sites

**Objective:** Pass empty string for media generation

**Files:**
- Modify: `internal/tools/builtin/media_client.go:377`

**Step 1: Find call site**

Use `search_files`:
```bash
grep -n "ResolveForAlias" internal/tools/builtin/media_client.go
```

**Step 2: Update call site**

Change from:
```go
resolver.ResolveForAlias("small")
```
to:
```go
resolver.ResolveForAlias("small", "")
```

**Step 3: Verify build**

Run: `go build ./internal/tools/...`
Expected: No errors

### Task 5: Update documentation

**Objective:** Document new configuration options

**Files:**
- Modify: `docs/configuration/llm.md`
- Modify: `docs/feature-model-aliases.md`

**Step 1: Update llm.md**

Add to the model_aliases section (after line 147):
```markdown
### Model Alias Options

Model aliases support additional configuration options:

- **default_model**: Optional model ID to revert to after cooldown expires. When set, the alias will return to this model instead of continuing round-robin. Format: `provider/model-id`.
- **balanced_sticky_requests**: When `true`, each concurrent caller (session/task) is pinned to a single model within the alias. This distributes load across models while maintaining consistency for each caller. When the pinned model enters cooldown, the pin is released and rotation continues normally.

Example:
```json5
"model_aliases": {
  "coder": {
    "models": ["zai/glm-5.2", "ollama/llama3.2"],
    "timeout": 30,
    "max_fails": 3,
    "default_model": "zai/glm-5.2",
    "balanced_sticky_requests": true
  }
}
```
```

**Step 2: Update feature-model-aliases.md**

Add new section after the Behavior section:

```markdown
## Advanced Features

### Default Model Reversion

When `default_model` is set, the alias reverts to the specified model after cooldown expires:

```json5
"model_aliases": {
  "coder": {
    "models": ["zai/glm-4.7", "ollama/llama3.2"],
    "default_model": "zai/glm-4.7",
    "timeout": 30,
    "max_fails": 3
  }
}
```

After a model failure and cooldown period, the alias returns to `default_model` instead of continuing round-robin.

### Sticky Request Distribution

When `balanced_sticky_requests` is enabled, each caller session is pinned to a single model:

```json5
"model_aliases": {
  "coder": {
    "models": ["zai/glm-5.2", "ollama/llama3.2", "zai/glm-4.7"],
    "balanced_sticky_requests": true,
    "timeout": 30,
    "max_fails": 3
  }
}
```

Benefits:
- Different sessions use different models (load distribution)
- Each session sees consistent model behavior
- Pins release automatically when the pinned model enters cooldown

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep - only what the tasks specify
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] Documentation accurately reflects new features

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] All call sites updated with correct callerKey
- [ ] Documentation accurately describes new features
- [ ] Code follows project conventions
- [ ] No bugs, no security issues
- [ ] No scope creep beyond specified tasks
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- Classifiers and analyzers don't need sticky behavior (they're short-lived)
- Agent loops DO need sticky behavior (long-running, benefit from model consistency)
- Empty string callerKey = no sticky pin (falls through to normal rotation)
- Documentation uses JSON5 format to match existing config style
