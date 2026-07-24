# Leaf 01-01: Verification Mode — Agent Spec Extension

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 01-adversarial-verification/orchestrator.md
**Scope:** Add verification configuration to agent definitions (front matter), daemon settings, and the agent spec type system.
**Dependencies:** None
**Estimated Context:** ~80K

## Interface Contract

This leaf exposes:
- `VerificationConfig` struct in `internal/agent/specs/`
- `verification` field parsed from agent JSON5 definitions
- `verification` section in daemon config (`internal/daemon/` or `internal/config/`)
- All built-in agent definitions updated with `verification` front matter

## Tasks

### Task 1: Define VerificationConfig type

**File:** `internal/agent/specs/verification.go` (new)

```go
package specs

// VerificationConfig controls adversarial verification behavior per agent.
type VerificationConfig struct {
    // Enabled controls whether verification runs for this agent.
    // Default: true. Set false to disable (e.g., for the verifier agent itself,
    // or when verification happens at a higher orchestration level).
    Enabled bool `json:"enabled"`

    // Model overrides the model used for the verifier.
    // Empty string = inherit the agent's model.
    // Set to a model ID (e.g., "claude-sonnet-4-20250514") to use a different
    // model that may be better at verification than implementation.
    Model string `json:"model"`

    // AutoTrigger enables automatic verification after non-trivial changes.
    // Default: true. When false, verification only runs when explicitly requested.
    AutoTrigger bool `json:"auto_trigger"`

    // MaxFixLoops is the maximum number of auto-fix iterations before
    // escalating to the user. Default: 3. Configurable in daemon settings.
    MaxFixLoops int `json:"max_fix_loops"`
}

// DefaultVerificationConfig returns the default verification settings.
func DefaultVerificationConfig() VerificationConfig {
    return VerificationConfig{
        Enabled:     true,
        Model:       "",
        AutoTrigger: true,
        MaxFixLoops: 3,
    }
}

// EffectiveModel returns the model to use for verification.
// If Model is empty, returns the provided fallback (the agent's model).
func (v VerificationConfig) EffectiveModel(agentModel string) string {
    if v.Model != "" {
        return v.Model
    }
    return agentModel
}
```

### Task 2: Wire VerificationConfig into AgentSpec

**File:** `internal/agent/specs/spec.go` (or wherever AgentSpec is defined)

Read the existing AgentSpec struct. Add:
```go
Verification VerificationConfig `json:"verification"`
```

In the spec loading/parsing function, default to `DefaultVerificationConfig()` when the field is absent (backward compatibility with existing agent definitions that lack the field).

### Task 3: Add daemon-level verification defaults

**File:** `internal/daemon/config.go` or `internal/config/config.go` (wherever daemon config is defined)

Add a `VerificationDefaults` struct:
```go
type VerificationDefaults struct {
    Enabled              bool   `json:"enabled"`
    DefaultModel         string `json:"default_model"`
    MaxFixLoops          int    `json:"max_fix_loops"`
    AutoTriggerThreshold int    `json:"auto_trigger_threshold"` // file edits before auto-trigger
}
```

Default values: `Enabled: true, DefaultModel: "", MaxFixLoops: 3, AutoTriggerThreshold: 3`.

Wire into the daemon config loading so `meept.json5` can override:
```json5
{
  "verification": {
    "enabled": true,
    "default_model": "",
    "max_fix_loops": 3,
    "auto_trigger_threshold": 3
  }
}
```

The per-agent `VerificationConfig` overrides daemon defaults. Resolution order: agent field > daemon default > hardcoded default.

### Task 4: Update all built-in agent definitions

**Files:** All `config/agents/*.json5` files

Add `verification` front matter to every built-in agent. Examples:

For the coder agent:
```json5
{
  // ... existing fields ...
  "verification": {
    "enabled": true,
    "model": "",
    "auto_trigger": true,
    "max_fix_loops": 3
  }
}
```

For the skeptic agent (should NOT verify itself):
```json5
{
  // ... existing fields ...
  "verification": {
    "enabled": false
  }
}
```

For agents where verification might happen at a higher level (orchestrator, planner):
```json5
{
  "verification": {
    "enabled": true,
    "auto_trigger": false
  }
}
```

List all built-in agent files first (`find config/agents/ -name '*.json5'`), then update each.

### Task 5: Add CLI visibility

**File:** `cmd/meept/agents.go` (or wherever `agents show` is implemented)

When displaying an agent definition (`meept agents show <id>`), include the verification config in the output. This satisfies the wiring requirement.

### Task 6: Tests

**File:** `internal/agent/specs/verification_test.go` (new)

Table-driven tests:
- `TestDefaultVerificationConfig` — verify defaults
- `TestEffectiveModel` — empty returns fallback, non-empty returns override
- `TestVerificationConfigParsing` — parse JSON5 with and without verification field
- `TestVerificationConfigBackwardCompat` — agent definition without verification field gets defaults
- `TestDaemonDefaultsOverride` — daemon config overrides hardcoded defaults
- `TestAgentOverridesDaemon` — agent field overrides daemon default

## Self-Verification Checklist

- [ ] `go build ./internal/agent/specs/...` compiles
- [ ] `go test ./internal/agent/specs/... -race` passes
- [ ] All `config/agents/*.json5` files parse without error
- [ ] `go build ./cmd/meept/...` compiles (CLI wiring)
- [ ] No unused imports or functions
- [ ] Setter methods have nil guards (if any Set* methods added)

## Review Checklist (for orchestrator)

- [ ] VerificationConfig has all 4 fields with correct JSON tags
- [ ] DefaultVerificationConfig matches documented defaults
- [ ] Backward compatibility: existing agent definitions without verification field still load
- [ ] All built-in agents updated (count matches `find config/agents/ -name '*.json5' | wc -l`)
- [ ] Daemon config section documented
- [ ] CLI shows verification config in `agents show` output
- [ ] Tests cover parsing, defaults, override chain, backward compat
