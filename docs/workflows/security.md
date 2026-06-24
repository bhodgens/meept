# Security Engine

## Overview
Meept implements multiple security layers including input sanitization, permission-based tool access, shell command scanning, audit logging, and adversarial input defense. The security engine protects against prompt injection, data exfiltration, and unauthorized access.

**See Also:**
- [Adversarial Input Defense](adversarial-input-defense.md) - Defense-in-depth protection for web fetches, file reads, MCP tools, and memory retrieval
- [Taint Tracking](taint-tracking.md) - Lattice-based information flow security

## Problem
Autonomous agents require robust security to prevent misuse and protect sensitive data. The security engine addresses:
- Prompt injection attacks
- Unauthorized tool access
- Dangerous shell commands
- Data leakage prevention

## Behavior

### Input Sanitization
- **Pattern-based Detection**: Blocks known prompt injection patterns
- **Three Strictness Levels**:
  - **Permissive**: Minimal blocking, high usability
  - **Standard**: Balanced security/usability (default)
  - **Strict**: Maximum security, may block legitimate requests
- **Configurable Confirmation**: High/critical risk operations require user confirmation

### Security Engine (SQLite-backed)
- **Permission Checks**: Tool access controlled by risk levels
- **Tool Gating**: High-risk tools require explicit permissions
- **Audit Logging**: All sensitive operations logged
- **Session Tracking**: User session management
- **Override Matching**: Configurable pattern matching for permission overrides (legacy lenient vs strict glob/exact)

### Tirith Shell Scanning
- **Pre-execution Analysis**: Shell commands scanned before execution
- **Dangerous Pattern Blocking**: Blocks known malicious patterns
- **Configurable Binary**: Custom tirith binary path support

### Taint Tracking
- **Lattice-based Propagation**: Tracks data provenance through operations
- **Taint Labels**: `UserInput`, `Secret`, `Untrusted`, `External`, `Shell`
- **Sink Enforcement**: Blocks tainted data at sensitive operations
- **Implementation**: `internal/security/taint/taint.go`

### Evidence-Based Validation
- **Claim-Evidence Matching**: Verifies claims match evidence types
- **Ground-Truth Verification**: Filesystem, API, database validation
- **Validator Coverage**: 14 tool hints with type-specific validators

### Adversarial Input Defense (NEW)
- **Boundary Markers**: `<<<USER_INPUT>>>`, `<<<TOOL_OUTPUT:{name}>>>` wrappers
- **Output Sanitization**: Scans tool results for injection patterns
- **Taint Propagation**: Marks web fetches, file reads with provenance labels
- **Implementation**: `internal/agent/loop.go`, `internal/tools/builtin/*.go`
- **Full Documentation**: [Adversarial Input Defense](adversarial-input-defense.md)

## Configuration

```toml
[security]
sanitize_inputs = true
sanitize_strictness = "standard"
llm_filter_external = false
require_confirmation_high = true
require_confirmation_critical = true
block_financial = true
allowed_paths = ["~/*"]
blocked_paths = ["~/.ssh/*", "~/.gnupg/*"]

# Output monitoring
monitor_output = true
redact_output = true

# Shell security
scan_shell_commands = true
tirith_binary = "tirith"

# Audit logging
enable_audit_log = false
audit_db_path = "~/.meept/audit.db"

# Override matching (opt-in strict mode)
# When true, uses strict glob/exact matching for permission overrides
# When false (default), uses lenient three-strategy cascade
strict_override_matching = false

# Taint tracking
enable_taint_tracking = true
taint_db_path = "~/.meept/taint.db"
```

## Observability

### Logging
- Input sanitization events
- Permission check results
- Shell command scanning
- Taint tracking violations
- Audit log entries

### Metrics
- Sanitization block rate
- Permission denial rate
- Shell command approval rate
- Taint violation incidents

### Debug Info
- Current security settings
- Active taint labels
- Permission mappings
- Audit log status

## Edge Cases

### False Positive Sanitization
- Legitimate requests blocked by patterns
- User can override with confirmation
- Patterns refined based on feedback

### Permission Escalation Attempt
- Unauthorized tool access blocked
- Audit log records attempt
- User notified of security violation

### Tirith Scan Failure
- Shell command execution blocked
- Fallback to safer alternatives
- Logs scan failure for investigation

### Taint Propagation Error
- Taint labels incorrectly propagated
- Security engine blocks uncertain operations
- Manual review required for resolution