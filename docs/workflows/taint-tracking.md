# Taint Tracking

## Overview
Taint tracking implements lattice-based information flow security, tracking data provenance through operations and preventing sensitive data leakage. The system uses taint labels and sink enforcement to protect against data exfiltration and prompt injection.

**See Also:**
- [Adversarial Input Defense](adversarial-input-defense.md) - How taint tracking integrates with boundary markers and sanitization
- [Security Engine](security.md) - Overall security architecture

## Problem
Unauthorized data flow can lead to security breaches. Taint tracking addresses:
- Data provenance tracking
- Sensitive information protection
- Cross-boundary data flow control
- Security policy enforcement

## Behavior

### Lattice-Based Propagation
- **Taint Labels**: Hierarchical security levels
- **Propagation Rules**: Taints flow through operations
- **Join Operation**: Combines taints from multiple sources
- **Subsumption**: Higher taint levels dominate

### Taint Labels
- **`TaintUserInput`**: From direct user input
- **`TaintSecret`**: API keys, tokens, passwords
- **`TaintUntrusted`**: From sandboxed/untrusted agents
- **`TaintExternal`**: From network requests
- **`TaintShell`**: Data destined for shell execution

### Taint Sinks
- **`ShellExecSink`**: Blocks external, untrusted, user input
- **`NetFetchSink`**: Blocks secrets from URLs
- **`AgentMessageSink`**: Blocks secrets from cross-agent messages

### Declassification
- **Explicit Sanitization**: Manual taint removal after validation
- **Safe Operations**: Certain operations automatically declassify
- **User Approval**: High-risk declassification requires confirmation

## Configuration

```toml
[security.taint]
enabled = true
taint_db_path = "~/.meept/taint.db"

[security.taint.labels]
user_input = 10
secret = 20
untrusted = 15
external = 12
shell = 8

[security.taint.sinks]
block_user_input_shell = true
block_secret_network = true
block_untrusted_agent = true

[security.taint.declassification]
require_approval_high = true
safe_operations = ["sanitize", "validate", "hash"]
```

## Observability

### Logging
- Taint marking operations
- Propagation events
- Sink violation attempts
- Declassification decisions

### Metrics
- Taint violation incidents
- Propagation chain lengths
- Sink check performance
- Declassification rates

### Debug Info
- Active taint labels
- Propagation rules
- Sink configurations
- Declassification policies

## Edge Cases

### False Positive Blocking
- Legitimate operations blocked
- Manual override available
- Policy refinement based on patterns

### Taint Propagation Error
- Incorrect taint assignment
- Security engine blocks uncertain operations
- Manual review required

### Sink Configuration Conflict
- Conflicting sink rules
- Conservative blocking applied
- Configuration validation

### Performance Impact
- Taint tracking overhead
- Optimization for critical paths
- Selective enabling for sensitive operations