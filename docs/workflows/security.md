# Security Engine

## Overview
Meept implements multiple security layers including input sanitization, permission-based tool access, shell command scanning, and audit logging. The security engine protects against prompt injection, data exfiltration, and unauthorized access.

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

### Tirith Shell Scanning
- **Pre-execution Analysis**: Shell commands scanned before execution
- **Dangerous Pattern Blocking**: Blocks known malicious patterns
- **Configurable Binary**: Custom tirith binary path support

### Taint Tracking (NEW)
- **Lattice-based Propagation**: Tracks data provenance through operations
- **Taint Labels**: `UserInput`, `Secret`, `Untrusted`, `External`, `Shell`
- **Sink Enforcement**: Blocks tainted data at sensitive operations

### Evidence-Based Validation (NEW)
- **Claim-Evidence Matching**: Verifies claims match evidence types
- **Ground-Truth Verification**: Filesystem, API, database validation
- **Validator Coverage**: 14 tool hints with type-specific validators

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