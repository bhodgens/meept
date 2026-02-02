# Security Permissions & Tool Risk Classification System

## Overview

Replace the current hardcoded `_BUILTIN_RULES` dict and regex-based shell
classification with a **data-driven, SQLite-backed rule engine** that:

1. Stores all tool risk rules, command classifications, and access policies in a
   SQLite database (`~/.meept/security.db`).
2. Pre-populates the database with best-practice defaults derived from LLM
   safety research (NIST AI RMF, OWASP LLM Top 10, Anthropic usage policy
   patterns).
3. Exposes a minimal **contextual scope** to the LLM -- the agent sees only
   the verdict for the current action, not the full rule set.
4. Integrates with the **memory system** for a permissionary model where
   creator approvals/denials evolve the policy over time.
5. Logs every permission decision to the database for audit and drift
   detection.

---

## Design Principles

### P1: Least Privilege by Default
Every tool and command starts at the highest safe restriction level.
Permissions are **granted** explicitly; they are never assumed.

### P2: Defence in Depth
Multiple independent checks run in series:
1. Static rule lookup (database)
2. Contextual risk analysis (command inspection, path analysis)
3. Memory-based policy overrides (creator permissions)
4. Confirmation gate (for HIGH/CRITICAL)

Failure at any layer is a deny.

### P3: Minimal Contextual Scope
The LLM receives only:
- "Permitted" / "Denied: {one-line reason}"
- For HIGH: "Requires confirmation: {description}"

It never sees the rule database, the scoring internals, or the full policy
set. This prevents prompt-injection attacks that attempt to argue against
security rules.

### P4: Auditable by Design
Every decision (allow, deny, escalate) is logged with full context:
timestamp, action, arguments, rule that matched, effective risk level,
and whether memory override was applied. The creator can query or export
this log at any time.

### P5: Memory-Augmented Permissionary Model
When the creator explicitly approves or denies a specific action pattern,
that decision is stored both in the rule database (as an override) and in
the memory system. On subsequent encounters:
- If the creator said "yes" to a pattern N times without caveat, the agent
  treats it as pre-approved (up to a configurable trust threshold).
- If the creator said "no", the agent blocks it and remembers why.
- Overrides decay: after a configurable period (default 30 days) or after
  a configurable number of uses (default 50), the override expires and the
  action returns to requiring confirmation.

### P6: Immutable Deny List
Certain actions are **never** overridable regardless of creator permission:
- Financial transactions
- Credential exfiltration
- Self-replication
- System destruction (rm -rf /, mkfs, dd if=/dev/zero)
These are enforced at the database level with a `immutable` flag.

---

## Database Schema

### `tool_rules` -- base risk classification for each tool

```sql
CREATE TABLE tool_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name   TEXT NOT NULL,             -- e.g. "shell", "file_write"
    action      TEXT NOT NULL,             -- permission category: "shell_execute", "file_write"
    risk_level  INTEGER NOT NULL DEFAULT 2, -- 0=SAFE, 1=LOW, 2=MEDIUM, 3=HIGH, 4=CRITICAL
    description TEXT NOT NULL DEFAULT '',
    requires_confirmation BOOLEAN NOT NULL DEFAULT 0,
    immutable   BOOLEAN NOT NULL DEFAULT 0, -- cannot be overridden by creator
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(tool_name, action)
);
```

### `command_patterns` -- risk classification for shell commands

```sql
CREATE TABLE command_patterns (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern     TEXT NOT NULL,             -- regex or glob pattern
    pattern_type TEXT NOT NULL DEFAULT 'regex', -- 'regex', 'glob', 'exact'
    risk_level  INTEGER NOT NULL,
    category    TEXT NOT NULL DEFAULT 'general', -- 'destructive', 'network', 'install', 'read_only', 'code_execution', etc.
    description TEXT NOT NULL DEFAULT '',
    immutable   BOOLEAN NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(pattern, pattern_type)
);
```

### `path_rules` -- file path access control

```sql
CREATE TABLE path_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern     TEXT NOT NULL,             -- glob pattern for paths
    rule_type   TEXT NOT NULL,             -- 'allow' or 'block'
    risk_level  INTEGER NOT NULL DEFAULT 2,
    description TEXT NOT NULL DEFAULT '',
    immutable   BOOLEAN NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(pattern, rule_type)
);
```

### `permission_overrides` -- creator approval/denial memory

```sql
CREATE TABLE permission_overrides (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    action          TEXT NOT NULL,          -- action category
    pattern         TEXT NOT NULL DEFAULT '*', -- specific pattern (command, path, etc.)
    decision        TEXT NOT NULL,          -- 'allow' or 'deny'
    reason          TEXT NOT NULL DEFAULT '', -- why the creator decided this
    usage_count     INTEGER NOT NULL DEFAULT 0,
    max_uses        INTEGER NOT NULL DEFAULT 50, -- expire after N uses (0=unlimited)
    expires_at      TEXT,                   -- ISO datetime, NULL=no expiry
    conversation_id TEXT,                   -- which conversation created this
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
```

### `decision_log` -- audit trail of every permission check

```sql
CREATE TABLE decision_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    action          TEXT NOT NULL,
    tool_name       TEXT NOT NULL DEFAULT '',
    details_json    TEXT NOT NULL DEFAULT '{}', -- serialized arguments
    risk_level      INTEGER NOT NULL,
    decision        TEXT NOT NULL,          -- 'allow', 'deny', 'escalate'
    reason          TEXT NOT NULL DEFAULT '',
    rule_source     TEXT NOT NULL DEFAULT '', -- 'base_rule', 'command_pattern', 'path_rule', 'override', 'immutable', 'confirmation_gate'
    override_id     INTEGER,               -- FK to permission_overrides if used
    conversation_id TEXT
);
```

### `financial_patterns` -- dedicated table for financial operation detection

```sql
CREATE TABLE financial_patterns (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern     TEXT NOT NULL,
    pattern_type TEXT NOT NULL DEFAULT 'regex',
    description TEXT NOT NULL DEFAULT '',
    immutable   BOOLEAN NOT NULL DEFAULT 1,
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    UNIQUE(pattern, pattern_type)
);
```

---

## Pre-populated Best-Practice Rules

### Tool Risk Levels

| Tool | Action | Risk | Confirm? | Immutable? | Rationale |
|------|--------|------|----------|------------|-----------|
| file_read | file_read | SAFE (0) | No | No | Read-only, contained |
| file_write | file_write | MEDIUM (2) | No | No | Creates/modifies files |
| file_delete | file_delete | HIGH (3) | Yes | No | Destructive, irreversible |
| shell | shell_execute | MEDIUM (2) | No | No | Base; elevated per-command |
| network | network_request | LOW (1) | No | No | Outbound HTTP to configured hosts |
| send_message | send_message | MEDIUM (2) | No | No | External communication |
| install_package | install_package | HIGH (3) | Yes | No | System modification |
| system_modify | system_modify | CRITICAL (4) | Yes | Yes | OS-level changes |

### Command Pattern Classifications

#### CRITICAL (4) -- Immutable, always blocked
- `rm\s+-rf\s+/` -- recursive root delete
- `mkfs` -- filesystem format
- `dd\s+if=/dev/(zero|urandom)\s+of=/dev/` -- disk overwrite
- `:(){ :\|:& };:` -- fork bomb
- `shutdown|reboot|halt|poweroff` -- system power
- `init\s+[06]` -- runlevel change

#### HIGH (3) -- Requires confirmation
- `rm\s+-rf` -- recursive delete (non-root)
- `chmod\s+-R|chown\s+-R` -- recursive permission changes
- `systemctl\s+(stop|disable|mask)` -- service disruption
- `kill\s+-9` -- force kill
- `iptables|nft|ufw` -- firewall modification
- `deluser|userdel|groupdel` -- user management
- `crontab\s+-e|-r` -- cron modification
- `pip\s+install` -- package installation
- `npm\s+install\s+-g` -- global npm install
- `cargo\s+install` -- binary installation
- `brew\s+install|apt\s+install|dnf\s+install` -- system package install
- `docker\s+run|docker\s+exec` -- container execution
- `curl\s+.*\|\s*(bash|sh|python)` -- pipe-to-shell

#### MEDIUM (2) -- Allowed, logged
- `python|python3` -- code execution (elevated from original LOW)
- `node|deno|bun` -- JS runtime execution
- `pip\s+list|pip\s+show` -- pip read-only
- `npm\s+ls|npm\s+list` -- npm read-only
- `make` -- build execution
- `git\s+(push|reset|rebase|force)` -- destructive git
- `cargo\s+build|cargo\s+test` -- Rust build
- `ssh|scp|rsync` -- remote access

#### LOW (1) -- Allowed, minimal logging
- `git\s+(status|log|diff|branch|show)` -- read-only git
- `ls|dir|pwd|whoami|hostname|uname|date` -- system info
- `cat|head|tail|less|more|wc` -- file reading
- `grep|rg|find|fd|locate` -- file search
- `echo|printf` -- output
- `env|printenv` -- environment inspection
- `ps|top|htop|df|du|free` -- system monitoring

### Path Rules

#### Blocked (always)
- `~/.ssh/*` -- SSH keys
- `~/.gnupg/*` -- GPG keys
- `~/.aws/*` -- AWS credentials
- `~/.config/gcloud/*` -- GCP credentials
- `~/.kube/*` -- Kubernetes config
- `/etc/shadow` -- system passwords
- `/etc/passwd` -- user database
- `*/.env` -- environment files with secrets
- `*/credentials.json` -- generic credential files
- `*/.git/config` -- may contain tokens

#### Allowed (default scope)
- `~/*` -- user home (minus blocked paths)

### Financial Patterns (immutable)
- `transfer\s+(funds?|money|payment)`
- `send\s+(payment|money|funds?)`
- `wire\s+transfer`
- `purchase|buy\s+|sell\s+|trade\s+`
- `withdraw|deposit`
- `credit\s*card|debit\s*card`
- `bank\s*account|routing\s*number|swift|iban`
- `cryptocurrency|bitcoin|ethereum|wallet\s*address`
- `paypal|venmo|zelle|stripe`
- `invoice\s+payment|billing\s+charge`

---

## Permission Engine Architecture

### Class: `SecurityEngine`

Replaces the current `PermissionManager`. Single entry point for all
permission decisions.

```
SecurityEngine
  ├── __init__(config, db_path)
  │     └── Opens/creates security.db, runs migrations, seeds defaults
  ├── check(action, tool_name, details) -> PermissionDecision
  │     ├── 1. Immutable deny check (financial, destructive)
  │     ├── 2. Base rule lookup (tool_rules table)
  │     ├── 3. Context analysis (command_patterns, path_rules)
  │     ├── 4. Memory override check (permission_overrides)
  │     ├── 5. Confirmation gate (if risk >= threshold)
  │     └── 6. Log decision (decision_log)
  ├── record_override(action, pattern, decision, reason, conversation_id)
  │     └── Stores creator approval/denial for future use
  ├── get_context_for_llm(action, tool_name) -> str
  │     └── Returns minimal one-line context: "Permitted" or "Denied: reason"
  ├── query_log(filters) -> list[DecisionRecord]
  │     └── Audit query interface
  └── get_stats() -> SecurityStats
        └── Summary: allows, denies, overrides, top denied actions
```

### PermissionDecision dataclass

```python
@dataclass
class PermissionDecision:
    allowed: bool
    reason: str
    risk_level: RiskLevel
    rule_source: str          # which check produced this decision
    requires_confirmation: bool
    override_applied: bool    # was a memory override used?
    override_id: int | None
```

---

## Memory Integration

### How the permissionary model works

1. **Initial encounter**: Agent asks to run `python script.py`. Engine checks
   `command_patterns` -- `python` is MEDIUM risk. No override exists. Decision:
   "Allowed" (MEDIUM doesn't require confirmation by default).

2. **Creator says "don't run python scripts without asking me"**: Agent calls
   `record_override("shell_execute", "python.*", "deny", "Creator wants
   confirmation for Python execution", conv_id)`. Future `python` commands
   now require confirmation.

3. **Creator says "yes, run it" on confirmation**: Agent calls
   `record_override("shell_execute", "python script.py", "allow",
   "Creator approved this specific script", conv_id)`. The override has
   `max_uses=50` and `expires_at=now+30d`.

4. **Memory persistence**: The override is also stored in episodic memory:
   `"Creator approved running python script.py without confirmation
   (expires in 30 days, 50 uses remaining)"`

5. **Decay**: After 50 uses or 30 days, the override expires. The agent
   returns to the base rule and asks for confirmation again.

6. **Forgetting**: If the episodic memory is consolidated and the override
   summary is dropped, the database override still holds. The database is
   the source of truth; memory is supplementary context.

### Memory queries at decision time

When the engine checks for overrides, it also queries the memory system:
```python
memories = await memory.search(
    f"permission {action} {pattern}",
    limit=3,
)
```
If memories contain explicit creator instructions that haven't been
encoded as database overrides yet, the engine logs a suggestion:
"Memory suggests creator has a preference for this action -- consider
recording an explicit override."

---

## LLM Context Integration

The agent loop currently builds a system prompt and includes available
tools. The security engine adds a **security context block** injected
into the system prompt only when relevant:

```
# Security Context (current action)
- Action: shell_execute
- Command: python process_data.py
- Risk: MEDIUM
- Status: Permitted
- Note: Creator has approved Python execution 12 times (override active)
```

For denied actions:
```
# Security Context (current action)
- Action: file_delete
- Path: ~/.ssh/id_rsa
- Risk: BLOCKED
- Status: Denied - Path matches immutable block rule for SSH keys
- Do not attempt to work around this restriction.
```

The full rule set, pattern tables, and override history are **never**
included in the LLM context.

---

## Implementation Plan

### Step 1: Create `SecurityEngine` class
- File: `src/meept/security/engine.py`
- SQLite database management with `aiosqlite`
- Schema creation and migration
- Default rule seeding

### Step 2: Create seed data module
- File: `src/meept/security/seed_rules.py`
- All pre-populated rules as Python data structures
- Idempotent seeding (INSERT OR IGNORE)

### Step 3: Implement permission check pipeline
- In `engine.py`: the `check()` method with all 6 stages
- `PermissionDecision` dataclass

### Step 4: Implement override management
- `record_override()`, override expiry checking
- Memory system integration for storing/querying creator preferences

### Step 5: Implement decision logging
- Every check writes to `decision_log`
- `query_log()` and `get_stats()` methods

### Step 6: Wire into existing system
- Replace `PermissionManager` usage in `executor.py` and `loop.py`
- Update `permissions.py` to delegate to `SecurityEngine` (backward
  compatible: `PermissionManager` wraps `SecurityEngine`)
- Update config schema for new security options

### Step 7: Update tests
- New test file: `tests/test_security/test_engine.py`
- Test all rule types, override lifecycle, expiry, immutable rules
- Test command classification against the full pattern set

### Step 8: Add RPC methods
- `security.query_log` -- audit trail access
- `security.get_stats` -- permission statistics
- `security.record_override` -- manual override management
