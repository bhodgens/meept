# Security Architecture

Meept implements a comprehensive, defense-in-depth security architecture with multiple layers of protection.

## Security Layers

### 1. Input Sanitization

The `InputSanitizer` (`internal/security/sanitizer.go`) provides 27 prompt injection pattern detectors including:
- Instruction override attempts
- Role switch attacks  
- System marker injection
- Special token injection (ChatML, Llama, Phi, EOS)

**Strictness levels:**
- `Permissive` - Basic pattern matching
- `Standard` - Full pattern suite (default)
- `Strict` - Aggressive detection with social engineering patterns

### 2. PromptGuard

The `PromptGuard` (`internal/security/prompt_guard.go`) provides ongoing injection resistance during multi-turn conversations:

**Detection patterns:**
- **Zero-width characters** - `\u200B-\u200F`, `\uFEFF` (BOM)
- **Cyrillic confusables** - `\u0400-\u04FF` (Cyrillic characters resembling Latin)
- **Homoglyph attacks** - Arabic, Persian, Greek, Armenian, Georgian, CJK, Hangul scripts
- **Base64-encoded content** - Long base64 strings that may hide instructions
- **HTML entity encoding** - Numeric (`&#123;`) and named (`&lt;`) entities
- **Unicode escape sequences** - `\uXXXX` patterns
- **Mixed script attacks** - Text combining Latin with other scripts

**Protection mechanisms:**
- User input boundary wrapping
- Tool output boundary wrapping
- Safety reminder injection at intervals
- System prompt construction with security guardrails

### 3. Security Orchestrator

The `Orchestrator` (`internal/security/orchestrator.go`) coordinates all security components:

**Features:**
- Input sanitization pipeline
- Output monitoring for credential leakage
- Shell command scanning via Tirith
- Taint tracking for information flow

**Fail-closed behavior:**
When the Tirith scanner is unavailable, commands are **blocked** for safety (fail-closed, not fail-open).

```go
if !o.tirithScanner.IsAvailable(ctx) {
    o.logger.Warn("Tirith not available, blocking command execution for security")
    return true, false, "security scanner unavailable - command blocked for safety"
}
```

### 4. Tirith Scanner

External shell command security scanner providing:
- Destructive command detection
- Self-replication prevention
- Financial transaction patterns
- Network exfiltration detection

### 5. Path Fencing

The `FenceChecker` (`internal/security/fence.go`) restricts file system access:

**Security features:**
- Project root confinement
- AllowList/DenyList path rules
- **Symlink resolution security** - Returns error on unresolvable symlinks instead of bypassing (prevents directory escape attacks)

```go
func resolveSymlinks(path string) (string, error) {
    if evaled, err := filepath.EvalSymlinks(path); err == nil {
        return evaled, nil
    }
    // Walk up to find existing ancestor
    // Return error if symlinks cannot be resolved
    return "", fmt.Errorf("fence: cannot resolve symlinks for %q", path)
}
```

### 6. Taint Tracking

The taint tracker (`internal/security/taint/taint.go`) tracks information flow:

**Taint labels:**
- `TaintExternal` - Data from external sources (web fetches, user input)
- `TaintSecret` - Sensitive data (API keys, tokens, passwords)
- `TaintPrivacy` - Personal/sensitive information

**WebFetch taint tracking:**
When content is fetched from the web, it's marked as externally tainted:

```go
func (o *Orchestrator) MarkWebFetchedContent(content, url string) (string, *WebFetchResult) {
    tv := o.taintTracker.MarkWebFetchedContent(content, url)
    varName := "wf_" + replaceNonAlnum(url)
    o.taintTracker.Store(varName, tv)
    return tv.Value, &WebFetchResult{Content: tv.Value, TaintedValue: tv}
}
```

Shell commands referencing web-fetched variables are automatically blocked:
- `$wf_http___example_com_page`
- `${wf_http___example_com_page}`
- Plain variable name in command

**Taint propagation:**
- String concatenation merges taint labels
- Variable references preserve taint
- Sink checks block tainted data reaching dangerous operations (shell exec, network)

### 7. Security Engine

The `Engine` (`internal/security/engine.go`) provides SQLite-backed decision making:

**Features:**
- Rule-based action Allow/Block/Escalate
- Override management with expiration
- Audit logging for all decisions
- Financial pattern detection

### 8. TLS Configuration

Secure TLS configuration helpers:

**Note:** The `InsecureSkipVerify()` function was **removed** to prevent accidental insecure connections in production. Development-mode TLS should use transport-specific configuration.

## Security Tool Integration

### Filesystem Tools
- Path sanitization via `SanitizeInput()`
- Fence checking for path confinement
- Security orchestrator integration

### WebFetch Tool
- URL exfiltration checking via `CheckWebFetch()`
- Content taint marking
- Variable storage for tracking

### ShellExecuteTool
- Tirith scanning integration
- Taint tracking for command arguments
- Pattern-based detection

## Audit Trail

All security events are logged:
- Input sanitization events
- Output credential detection
- Command blocking decisions
- Taint violations
- Override grants

## Configuration

Security features are configurable via `meept.json5`:

```json5
{
  security: {
    sanitizeInputs: true,
    sanitizeStrictness: "standard",
    scanShellCommands: true,
    tirithBinary: "/path/to/tirith",
    enableAuditLog: true,
  },
  fence: {
    enabled: true,
    projectRoot: "/path/to/project",
    allowRules: [...],
    denyRules: [...],
  },
}
```
