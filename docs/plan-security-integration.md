# Plan: Security Integration

**Status:** Not Started
**Priority:** High
**Estimated Effort:** 2-3 days

---

## Current State

The security components exist but are **not integrated** into the active code paths:

| Component | File | Status |
|-----------|------|--------|
| InputSanitizer | `internal/security/sanitizer.go` | Implemented, not called |
| OutputMonitor | `internal/security/sanitizer.go` | Implemented, not called |
| PromptGuard | `internal/security/prompt_guard.go` | Implemented, not called |
| Tirith (shell scanner) | `internal/security/tirith.go` | Implemented, not called |
| SecurityEngine | `internal/security/engine.go` | Implemented, not used |
| PermissionChecker | `pkg/security/permissions.go` | **Active** - used in executor |

### What Exists

1. **InputSanitizer** (365 lines)
   - 3 strictness levels: Permissive, Standard, Strict
   - 20+ injection pattern detections (role switching, token injection, etc.)
   - Structural token escaping (ChatML, Llama, Phi tokens)
   - Role marker stripping

2. **OutputMonitor** (365 lines)
   - 15+ credential patterns (API keys, tokens, passwords, AWS, JWT, etc.)
   - Credential redaction
   - Warning generation

3. **PromptGuard**
   - Additional prompt injection detection
   - Severity scoring

4. **Tirith**
   - Shell command scanning
   - Dangerous command detection
   - Path validation

### What's Missing

The components are never called in the hot paths:
- `internal/agent/loop.go` - No sanitization before LLM calls
- `internal/agent/executor.go` - No Tirith check before shell execution
- No output monitoring of LLM responses

---

## Implementation Plan

### Phase 1: Wire InputSanitizer into Agent Loop

**File:** `internal/agent/loop.go`

**Changes:**

1. Add sanitizer to AgentLoop struct:
```go
type AgentLoop struct {
    // ... existing fields
    sanitizer *security.InputSanitizer
}
```

2. Initialize in constructor:
```go
func NewAgentLoop(...) *AgentLoop {
    loop := &AgentLoop{...}
    loop.sanitizer = security.NewInputSanitizer(security.StrictnessStandard)
    return loop
}
```

3. Call sanitizer before processing user input in `Run()`:
```go
func (l *AgentLoop) Run(ctx context.Context, userMessage string) (*Response, error) {
    // Sanitize user input
    result := l.sanitizer.Sanitize(userMessage)
    if result.WasModified {
        l.logger.Warn("input sanitized",
            "threats", result.ThreatsDetected,
            "original_len", len(userMessage),
            "clean_len", len(result.CleanText))
    }
    userMessage = result.CleanText

    // Continue with existing logic...
}
```

4. Add config option for strictness level:
```go
// In internal/config/schema.go
type SecurityConfig struct {
    SanitizeInputs     bool   `toml:"sanitize_inputs"`
    SanitizeStrictness string `toml:"sanitize_strictness"` // permissive, standard, strict
    // ...
}
```

### Phase 2: Wire OutputMonitor into Agent Loop

**File:** `internal/agent/loop.go`

**Changes:**

1. Add output monitor:
```go
type AgentLoop struct {
    // ... existing fields
    outputMonitor *security.OutputMonitor
}
```

2. Check LLM responses before returning:
```go
func (l *AgentLoop) runReasoningCycle(ctx context.Context) (*Response, error) {
    // ... existing LLM call ...

    // Scan output for credentials
    if l.outputMonitor != nil {
        scanResult := l.outputMonitor.Scan(response.Content)
        if scanResult.HasCredentials {
            l.logger.Warn("credentials detected in output",
                "warnings", scanResult.Warnings)
            // Option 1: Redact and continue
            response.Content = scanResult.RedactedText
            // Option 2: Block entirely (for high-security mode)
        }
    }

    return response, nil
}
```

### Phase 3: Wire Tirith into Shell Tool

**File:** `internal/tools/builtin/shell.go`

**Changes:**

1. Add Tirith scanner to shell tool:
```go
type ShellTool struct {
    // ... existing fields
    scanner *security.TirithScanner
}
```

2. Scan commands before execution:
```go
func (t *ShellTool) Execute(ctx context.Context, args map[string]any) (*tools.ToolResult, error) {
    command := args["command"].(string)

    // Scan command with Tirith
    if t.scanner != nil {
        scanResult := t.scanner.Scan(command)
        if scanResult.IsDangerous {
            return tools.NewErrorResult(fmt.Sprintf(
                "command blocked by security scanner: %s",
                scanResult.Reason)), nil
        }
        if scanResult.RequiresConfirmation {
            // Could integrate with permission system here
        }
    }

    // Continue with existing execution...
}
```

### Phase 4: Configuration Integration

**File:** `internal/daemon/components.go`

**Changes:**

1. Create security components during daemon startup:
```go
func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize security components
    var sanitizer *security.InputSanitizer
    var outputMonitor *security.OutputMonitor
    var tirithScanner *security.TirithScanner

    if cfg.Security.SanitizeInputs {
        strictness := parseStrictness(cfg.Security.SanitizeStrictness)
        sanitizer = security.NewInputSanitizer(strictness)
    }

    if cfg.Security.MonitorOutput {
        outputMonitor = security.NewOutputMonitor()
    }

    if cfg.Security.ScanShellCommands {
        tirithScanner = security.NewTirithScanner(cfg.Security)
    }

    // Pass to agent loop factory...
}
```

2. Update config schema:
```toml
[security]
sanitize_inputs = true
sanitize_strictness = "standard"  # permissive, standard, strict
monitor_output = true
scan_shell_commands = true
```

### Phase 5: Audit Logging

**File:** `internal/security/audit.go`

Wire audit logging into daemon:

1. Create audit logger during startup
2. Log all security decisions (blocked inputs, detected threats, etc.)
3. Provide RPC endpoint for security log queries

---

## Testing Plan

### Unit Tests

1. **Sanitizer tests** (already exist in `sanitizer_test.go`)
   - Verify all injection patterns are caught
   - Verify clean input passes through
   - Test each strictness level

2. **Integration tests** (new)
   - Test full agent loop with malicious input
   - Verify credentials are redacted
   - Verify shell commands are scanned

### Manual Testing

1. Send prompt injection attempts through TUI
2. Verify logging shows detection
3. Test with various strictness levels
4. Test shell command blocking

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/agent/loop.go` | Add sanitizer, output monitor |
| `internal/tools/builtin/shell.go` | Add Tirith scanner |
| `internal/daemon/components.go` | Initialize security components |
| `internal/config/schema.go` | Add security config options |
| `config/meept.toml` | Add security settings |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/agent/security.go` | Security integration helpers |
| `tests/integration/security_test.go` | Integration tests |

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| False positives blocking legitimate input | Default to Standard strictness, not Strict |
| Performance impact | Sanitizer is fast (regex-based), minimal overhead |
| Breaking existing functionality | Feature-flag all security checks, default to current behavior |
| Credential redaction breaking output | Only redact, don't block, by default |

---

## Success Criteria

1. All user input passes through sanitizer before LLM
2. All LLM output passes through output monitor
3. All shell commands pass through Tirith
4. Security events are logged
5. No regression in existing functionality
6. Tests pass
