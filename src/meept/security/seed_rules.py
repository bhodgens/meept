"""Pre-populated security rules for the SecurityEngine.

Contains best-practice defaults for tool risk levels, command patterns,
path rules, and financial patterns.  These are inserted idempotently
when the security database is first created.
"""

from __future__ import annotations

# ---------------------------------------------------------------------------
# Risk level constants (mirror RiskLevel enum values)
# ---------------------------------------------------------------------------

SAFE = 0
LOW = 1
MEDIUM = 2
HIGH = 3
CRITICAL = 4

# ---------------------------------------------------------------------------
# Tool rules: (tool_name, action, risk_level, description, requires_confirmation, immutable)
# ---------------------------------------------------------------------------

TOOL_RULES: list[tuple[str, str, int, str, bool, bool]] = [
    ("file_read", "file_read", SAFE, "Read a file from the filesystem", False, False),
    ("file_write", "file_write", MEDIUM, "Write or overwrite a file on the filesystem", False, False),
    ("file_delete", "file_delete", HIGH, "Permanently delete a file from the filesystem", True, False),
    ("shell", "shell_execute", MEDIUM, "Execute a shell command", False, False),
    ("network", "network_request", LOW, "Make an outbound HTTP/HTTPS request", False, False),
    ("send_message", "send_message", MEDIUM, "Send a message to a user or external service", False, False),
    ("install_package", "install_package", HIGH, "Install a software package on the system", True, False),
    ("system_modify", "system_modify", CRITICAL, "Modify system-level configuration or settings", True, True),
    ("list_directory", "file_read", SAFE, "List directory contents", False, False),
    ("web_fetch", "network_request", LOW, "Fetch content from a URL", False, False),
    ("web_search", "network_request", LOW, "Search the web", False, False),
]

# ---------------------------------------------------------------------------
# Command patterns: (pattern, pattern_type, risk_level, category, description, immutable)
# ---------------------------------------------------------------------------

COMMAND_PATTERNS: list[tuple[str, str, int, str, str, bool]] = [
    # CRITICAL (4) -- Immutable, always blocked
    (r"\brm\s+-rf\s+/(?!\S)", "regex", CRITICAL, "destructive", "Recursive delete from root", True),
    (r"\bmkfs\b", "regex", CRITICAL, "destructive", "Filesystem format", True),
    (r"\bdd\s+if=/dev/(zero|urandom)\s+of=/dev/", "regex", CRITICAL, "destructive", "Disk overwrite", True),
    (r":\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:", "regex", CRITICAL, "destructive", "Fork bomb", True),
    (r"\b(shutdown|reboot|halt|poweroff)\b", "regex", CRITICAL, "destructive", "System power control", True),
    (r"\binit\s+[06]\b", "regex", CRITICAL, "destructive", "Runlevel change", True),
    (r"\b>\s*/dev/sd[a-z]", "regex", CRITICAL, "destructive", "Direct device write", True),
    (r"\bchmod\s+777\s+/", "regex", CRITICAL, "destructive", "World-writable root permissions", True),

    # Self-replication detection (Plan P6)
    (r"\bgit\s+clone\b.*meept", "regex", CRITICAL, "self_replication", "Cloning meept repository", True),
    (r"\bpip\s+install\b.*meept", "regex", CRITICAL, "self_replication", "Installing meept package", True),
    (r"\bcp\b.*meept.*meept", "regex", CRITICAL, "self_replication", "Copying meept to create duplicate", True),
    (r"\bdocker\b.*meept", "regex", CRITICAL, "self_replication", "Docker operation involving meept", True),

    # HIGH (3) -- Requires confirmation
    (r"\brm\s+-rf\b", "regex", HIGH, "destructive", "Recursive forced delete", False),
    (r"\brm\s+-r\b", "regex", HIGH, "destructive", "Recursive delete", False),
    (r"\bchmod\s+-R\b", "regex", HIGH, "permissions", "Recursive permission change", False),
    (r"\bchown\s+-R\b", "regex", HIGH, "permissions", "Recursive ownership change", False),
    (r"\bsystemctl\s+(stop|disable|mask)\b", "regex", HIGH, "service", "Service disruption", False),
    (r"\bkill\s+-9\b", "regex", HIGH, "process", "Force kill process", False),
    (r"\b(iptables|nft|ufw)\b", "regex", HIGH, "network", "Firewall modification", False),
    (r"\b(deluser|userdel|groupdel)\b", "regex", HIGH, "user_mgmt", "User/group deletion", False),
    (r"\bcrontab\s+(-e|-r)\b", "regex", HIGH, "scheduler", "Cron table modification", False),
    (r"\bpip\s+install\b", "regex", HIGH, "install", "Python package installation", False),
    (r"\bnpm\s+install\s+-g\b", "regex", HIGH, "install", "Global npm package installation", False),
    (r"\bcargo\s+install\b", "regex", HIGH, "install", "Rust binary installation", False),
    (r"\b(brew|apt|apt-get|dnf|yum|pacman)\s+install\b", "regex", HIGH, "install", "System package installation", False),
    (r"\bdocker\s+(run|exec)\b", "regex", HIGH, "container", "Container execution", False),
    (r"\bcurl\s+.*\|\s*(bash|sh|python|python3)\b", "regex", HIGH, "code_execution", "Pipe-to-shell execution", False),
    (r"\bwget\s+.*\|\s*(bash|sh|python|python3)\b", "regex", HIGH, "code_execution", "Pipe-to-shell execution", False),
    (r"\bsudo\b", "regex", HIGH, "privilege", "Privilege escalation", False),
    (r"\bsu\s+-?\s*\w", "regex", HIGH, "privilege", "User switching", False),
    (r"\bnpm\s+install\b", "regex", HIGH, "install", "npm package installation", False),

    # MEDIUM (2) -- Allowed, logged
    (r"\b(python|python3)\b", "regex", MEDIUM, "code_execution", "Python interpreter execution", False),
    (r"\b(node|deno|bun)\b", "regex", MEDIUM, "code_execution", "JavaScript runtime execution", False),
    (r"\bruby\b", "regex", MEDIUM, "code_execution", "Ruby interpreter execution", False),
    (r"\bperl\b", "regex", MEDIUM, "code_execution", "Perl interpreter execution", False),
    (r"\bmake\b", "regex", MEDIUM, "build", "Build system execution", False),
    (r"\bgit\s+(push|reset|rebase|force-push|checkout\s+--)\b", "regex", MEDIUM, "vcs", "Destructive git operations", False),
    (r"\bcargo\s+(build|test|run)\b", "regex", MEDIUM, "build", "Rust build/test/run", False),
    (r"\b(ssh|scp|rsync)\b", "regex", MEDIUM, "network", "Remote access", False),
    (r"\bpip\s+(list|show|freeze)\b", "regex", MEDIUM, "read_only", "pip read-only commands", False),
    (r"\bnpm\s+(ls|list|info|view)\b", "regex", MEDIUM, "read_only", "npm read-only commands", False),
    (r"\btee\b", "regex", MEDIUM, "file_write", "Write to file via tee", False),
    (r"\bsed\s+-i\b", "regex", MEDIUM, "file_write", "In-place file edit", False),

    # LOW (1) -- Allowed, minimal logging
    (r"\bgit\s+(status|log|diff|branch|show|remote|tag|stash\s+list)\b", "regex", LOW, "vcs", "Read-only git operations", False),
    (r"\b(ls|dir|pwd|whoami|hostname|uname|date|uptime|id)\b", "regex", LOW, "system_info", "System information commands", False),
    (r"\b(cat|head|tail|less|more|wc|file|stat)\b", "regex", LOW, "file_read", "File reading commands", False),
    (r"\b(grep|rg|find|fd|locate|which|whereis|type)\b", "regex", LOW, "search", "File/command search", False),
    (r"\b(echo|printf)\b", "regex", LOW, "output", "Output commands", False),
    (r"\b(env|printenv|set)\b", "regex", LOW, "system_info", "Environment inspection", False),
    (r"\b(ps|top|htop|df|du|free|vmstat|iostat)\b", "regex", LOW, "monitoring", "System monitoring commands", False),
]

# ---------------------------------------------------------------------------
# Path rules: (pattern, rule_type, risk_level, description, immutable)
# ---------------------------------------------------------------------------

PATH_RULES: list[tuple[str, str, int, str, bool]] = [
    # Blocked paths (immutable)
    ("~/.ssh/*", "block", CRITICAL, "SSH keys and configuration", True),
    ("~/.gnupg/*", "block", CRITICAL, "GPG keys and configuration", True),
    ("~/.aws/*", "block", HIGH, "AWS credentials", True),
    ("~/.config/gcloud/*", "block", HIGH, "GCP credentials", True),
    ("~/.kube/*", "block", HIGH, "Kubernetes configuration", True),
    ("~/.docker/config.json", "block", HIGH, "Docker credentials", True),
    ("/etc/shadow", "block", CRITICAL, "System password file", True),
    ("/etc/passwd", "block", CRITICAL, "System user database", True),
    ("/etc/sudoers", "block", CRITICAL, "Sudo configuration", True),
    ("*/.env", "block", HIGH, "Environment files with secrets", False),
    ("*/.env.local", "block", HIGH, "Local environment files", False),
    ("*/credentials.json", "block", HIGH, "Credential files", False),
    ("*/secrets.yaml", "block", HIGH, "Secret configuration files", False),
    ("*/secrets.yml", "block", HIGH, "Secret configuration files", False),
    ("*/.git/config", "block", MEDIUM, "Git config may contain tokens", False),
    ("~/.meept/meept.toml", "block", MEDIUM, "Meept configuration (edit manually)", False),
    ("~/.meept/security.db", "block", CRITICAL, "Security database (immutable)", True),

    # Allowed paths
    ("~/*", "allow", SAFE, "User home directory (minus blocked paths)", False),
]

# ---------------------------------------------------------------------------
# Financial patterns: (pattern, pattern_type, description)
# All are immutable.
# ---------------------------------------------------------------------------

FINANCIAL_PATTERNS: list[tuple[str, str, str]] = [
    (r"\btransfer\s+(funds?|money|payment)", "regex", "Fund transfer"),
    (r"\bsend\s+(payment|money|funds?)", "regex", "Payment sending"),
    (r"\bwire\s+transfer\b", "regex", "Wire transfer"),
    (r"\b(purchase|buy\s+|sell\s+|trade\s+)", "regex", "Financial transaction"),
    (r"\b(withdraw|deposit)\b", "regex", "Bank withdrawal/deposit"),
    (r"\bcredit\s*card\b", "regex", "Credit card operation"),
    (r"\bdebit\s*card\b", "regex", "Debit card operation"),
    (r"\bbank\s*account\b", "regex", "Bank account operation"),
    (r"\b(routing\s*number|swift|iban)\b", "regex", "Banking identifiers"),
    (r"\b(cryptocurrency|bitcoin|ethereum|wallet\s*address)\b", "regex", "Cryptocurrency operation"),
    (r"\b(paypal|venmo|zelle|stripe|square)\b", "regex", "Payment platform"),
    (r"\binvoice\s+payment\b", "regex", "Invoice payment"),
    (r"\bbilling\s+charge\b", "regex", "Billing charge"),
]
