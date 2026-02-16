package security

// SeedData contains all pre-populated security rules.
type SeedData struct {
	ToolRules         []SeedToolRule
	CommandPatterns   []SeedCommandPattern
	PathRules         []SeedPathRule
	FinancialPatterns []SeedFinancialPattern
}

// SeedToolRule defines a pre-populated tool rule.
type SeedToolRule struct {
	ToolName             string
	Action               string
	RiskLevel            RiskLevel
	Description          string
	RequiresConfirmation bool
	Immutable            bool
}

// SeedCommandPattern defines a pre-populated command pattern.
type SeedCommandPattern struct {
	Pattern     string
	PatternType string
	RiskLevel   RiskLevel
	Category    string
	Description string
	Immutable   bool
}

// SeedPathRule defines a pre-populated path rule.
type SeedPathRule struct {
	Pattern     string
	RuleType    string // block, allow
	RiskLevel   RiskLevel
	Description string
	Immutable   bool
}

// SeedFinancialPattern defines a pre-populated financial pattern.
type SeedFinancialPattern struct {
	Pattern     string
	PatternType string
	Description string
}

// SeedRules returns all pre-populated security rules (146 rules total).
func SeedRules() SeedData {
	return SeedData{
		ToolRules:         seedToolRules(),
		CommandPatterns:   seedCommandPatterns(),
		PathRules:         seedPathRules(),
		FinancialPatterns: seedFinancialPatterns(),
	}
}

// seedToolRules returns pre-populated tool rules (11 rules).
func seedToolRules() []SeedToolRule {
	return []SeedToolRule{
		// Basic file operations
		{ToolName: "file_read", Action: "file_read", RiskLevel: RiskSafe, Description: "Read a file from the filesystem", RequiresConfirmation: false, Immutable: false},
		{ToolName: "file_write", Action: "file_write", RiskLevel: RiskMedium, Description: "Write or overwrite a file on the filesystem", RequiresConfirmation: false, Immutable: false},
		{ToolName: "file_delete", Action: "file_delete", RiskLevel: RiskHigh, Description: "Permanently delete a file from the filesystem", RequiresConfirmation: true, Immutable: false},

		// Shell and network
		{ToolName: "shell", Action: "shell_execute", RiskLevel: RiskMedium, Description: "Execute a shell command", RequiresConfirmation: false, Immutable: false},
		{ToolName: "network", Action: "network_request", RiskLevel: RiskLow, Description: "Make an outbound HTTP/HTTPS request", RequiresConfirmation: false, Immutable: false},

		// Messaging and packages
		{ToolName: "send_message", Action: "send_message", RiskLevel: RiskMedium, Description: "Send a message to a user or external service", RequiresConfirmation: false, Immutable: false},
		{ToolName: "install_package", Action: "install_package", RiskLevel: RiskHigh, Description: "Install a software package on the system", RequiresConfirmation: true, Immutable: false},

		// System level
		{ToolName: "system_modify", Action: "system_modify", RiskLevel: RiskCritical, Description: "Modify system-level configuration or settings", RequiresConfirmation: true, Immutable: true},

		// Aliases
		{ToolName: "list_directory", Action: "file_read", RiskLevel: RiskSafe, Description: "List directory contents", RequiresConfirmation: false, Immutable: false},
		{ToolName: "web_fetch", Action: "network_request", RiskLevel: RiskLow, Description: "Fetch content from a URL", RequiresConfirmation: false, Immutable: false},
		{ToolName: "web_search", Action: "network_request", RiskLevel: RiskLow, Description: "Search the web", RequiresConfirmation: false, Immutable: false},
	}
}

// seedCommandPatterns returns pre-populated command patterns (60 rules).
func seedCommandPatterns() []SeedCommandPattern {
	return []SeedCommandPattern{
		// CRITICAL (4) -- Immutable, always blocked
		{Pattern: `\brm\s+-rf\s+/(?!\S)`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "Recursive delete from root", Immutable: true},
		{Pattern: `\bmkfs\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "Filesystem format", Immutable: true},
		{Pattern: `\bdd\s+if=/dev/(zero|urandom)\s+of=/dev/`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "Disk overwrite", Immutable: true},
		{Pattern: `:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "Fork bomb", Immutable: true},
		{Pattern: `\b(shutdown|reboot|halt|poweroff)\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "System power control", Immutable: true},
		{Pattern: `\binit\s+[06]\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "Runlevel change", Immutable: true},
		{Pattern: `\b>\s*/dev/sd[a-z]`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "Direct device write", Immutable: true},
		{Pattern: `\bchmod\s+777\s+/`, PatternType: "regex", RiskLevel: RiskCritical, Category: "destructive", Description: "World-writable root permissions", Immutable: true},

		// Self-replication detection
		{Pattern: `\bgit\s+clone\b.*meept`, PatternType: "regex", RiskLevel: RiskCritical, Category: "self_replication", Description: "Cloning meept repository", Immutable: true},
		{Pattern: `\bpip\s+install\b.*meept`, PatternType: "regex", RiskLevel: RiskCritical, Category: "self_replication", Description: "Installing meept package", Immutable: true},
		{Pattern: `\bcp\b.*meept.*meept`, PatternType: "regex", RiskLevel: RiskCritical, Category: "self_replication", Description: "Copying meept to create duplicate", Immutable: true},
		{Pattern: `\bdocker\b.*meept`, PatternType: "regex", RiskLevel: RiskCritical, Category: "self_replication", Description: "Docker operation involving meept", Immutable: true},

		// Additional critical patterns
		{Pattern: `\bkernelctl\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "system", Description: "Kernel control", Immutable: true},
		{Pattern: `\bmodprobe\s+-r\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "system", Description: "Kernel module removal", Immutable: true},
		{Pattern: `\bsysctl\s+-w\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "system", Description: "Kernel parameter modification", Immutable: true},
		{Pattern: `\b>\s*/etc/passwd\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "system", Description: "Password file overwrite", Immutable: true},
		{Pattern: `\b>\s*/etc/shadow\b`, PatternType: "regex", RiskLevel: RiskCritical, Category: "system", Description: "Shadow file overwrite", Immutable: true},

		// HIGH (3) -- Requires confirmation
		{Pattern: `\brm\s+-rf\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "destructive", Description: "Recursive forced delete", Immutable: false},
		{Pattern: `\brm\s+-r\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "destructive", Description: "Recursive delete", Immutable: false},
		{Pattern: `\bchmod\s+-R\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "permissions", Description: "Recursive permission change", Immutable: false},
		{Pattern: `\bchown\s+-R\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "permissions", Description: "Recursive ownership change", Immutable: false},
		{Pattern: `\bsystemctl\s+(stop|disable|mask)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "service", Description: "Service disruption", Immutable: false},
		{Pattern: `\bkill\s+-9\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "process", Description: "Force kill process", Immutable: false},
		{Pattern: `\b(iptables|nft|ufw)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "network", Description: "Firewall modification", Immutable: false},
		{Pattern: `\b(deluser|userdel|groupdel)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "user_mgmt", Description: "User/group deletion", Immutable: false},
		{Pattern: `\bcrontab\s+(-e|-r)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "scheduler", Description: "Cron table modification", Immutable: false},
		{Pattern: `\bpip\s+install\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "install", Description: "Python package installation", Immutable: false},
		{Pattern: `\bnpm\s+install\s+-g\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "install", Description: "Global npm package installation", Immutable: false},
		{Pattern: `\bcargo\s+install\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "install", Description: "Rust binary installation", Immutable: false},
		{Pattern: `\b(brew|apt|apt-get|dnf|yum|pacman)\s+install\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "install", Description: "System package installation", Immutable: false},
		{Pattern: `\bdocker\s+(run|exec)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "container", Description: "Container execution", Immutable: false},
		{Pattern: `\bcurl\s+.*\|\s*(bash|sh|python|python3)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "code_execution", Description: "Pipe-to-shell execution", Immutable: false},
		{Pattern: `\bwget\s+.*\|\s*(bash|sh|python|python3)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "code_execution", Description: "Pipe-to-shell execution", Immutable: false},
		{Pattern: `\bsudo\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "privilege", Description: "Privilege escalation", Immutable: false},
		{Pattern: `\bsu\s+-?\s*\w`, PatternType: "regex", RiskLevel: RiskHigh, Category: "privilege", Description: "User switching", Immutable: false},
		{Pattern: `\bnpm\s+install\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "install", Description: "npm package installation", Immutable: false},
		{Pattern: `\blaunchctl\s+(unload|remove)\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "service", Description: "macOS service management", Immutable: false},
		{Pattern: `\bpkill\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "process", Description: "Pattern-based process kill", Immutable: false},
		{Pattern: `\bkillall\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "process", Description: "Kill all processes by name", Immutable: false},
		{Pattern: `\bgit\s+push\s+.*--force\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "vcs", Description: "Force push to git", Immutable: false},
		{Pattern: `\bgit\s+reset\s+--hard\b`, PatternType: "regex", RiskLevel: RiskHigh, Category: "vcs", Description: "Hard git reset", Immutable: false},

		// MEDIUM (2) -- Allowed, logged
		{Pattern: `\b(python|python3)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "code_execution", Description: "Python interpreter execution", Immutable: false},
		{Pattern: `\b(node|deno|bun)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "code_execution", Description: "JavaScript runtime execution", Immutable: false},
		{Pattern: `\bruby\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "code_execution", Description: "Ruby interpreter execution", Immutable: false},
		{Pattern: `\bperl\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "code_execution", Description: "Perl interpreter execution", Immutable: false},
		{Pattern: `\bmake\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "build", Description: "Build system execution", Immutable: false},
		{Pattern: `\bgit\s+(push|reset|rebase|force-push|checkout\s+--)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "vcs", Description: "Destructive git operations", Immutable: false},
		{Pattern: `\bcargo\s+(build|test|run)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "build", Description: "Rust build/test/run", Immutable: false},
		{Pattern: `\b(ssh|scp|rsync)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "network", Description: "Remote access", Immutable: false},
		{Pattern: `\bpip\s+(list|show|freeze)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "read_only", Description: "pip read-only commands", Immutable: false},
		{Pattern: `\bnpm\s+(ls|list|info|view)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "read_only", Description: "npm read-only commands", Immutable: false},
		{Pattern: `\btee\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "file_write", Description: "Write to file via tee", Immutable: false},
		{Pattern: `\bsed\s+-i\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "file_write", Description: "In-place file edit", Immutable: false},
		{Pattern: `\bgo\s+(build|run|install)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "build", Description: "Go build/run/install", Immutable: false},
		{Pattern: `\bzig\s+(build|run)\b`, PatternType: "regex", RiskLevel: RiskMedium, Category: "build", Description: "Zig build/run", Immutable: false},

		// LOW (1) -- Allowed, minimal logging
		{Pattern: `\bgit\s+(status|log|diff|branch|show|remote|tag|stash\s+list)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "vcs", Description: "Read-only git operations", Immutable: false},
		{Pattern: `\b(ls|dir|pwd|whoami|hostname|uname|date|uptime|id)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "system_info", Description: "System information commands", Immutable: false},
		{Pattern: `\b(cat|head|tail|less|more|wc|file|stat)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "file_read", Description: "File reading commands", Immutable: false},
		{Pattern: `\b(grep|rg|find|fd|locate|which|whereis|type)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "search", Description: "File/command search", Immutable: false},
		{Pattern: `\b(echo|printf)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "output", Description: "Output commands", Immutable: false},
		{Pattern: `\b(env|printenv|set)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "system_info", Description: "Environment inspection", Immutable: false},
		{Pattern: `\b(ps|top|htop|df|du|free|vmstat|iostat)\b`, PatternType: "regex", RiskLevel: RiskLow, Category: "monitoring", Description: "System monitoring commands", Immutable: false},
	}
}

// seedPathRules returns pre-populated path rules (29 rules).
func seedPathRules() []SeedPathRule {
	return []SeedPathRule{
		// Blocked paths (immutable) - Critical
		{Pattern: "~/.ssh/*", RuleType: "block", RiskLevel: RiskCritical, Description: "SSH keys and configuration", Immutable: true},
		{Pattern: "~/.gnupg/*", RuleType: "block", RiskLevel: RiskCritical, Description: "GPG keys and configuration", Immutable: true},
		{Pattern: "/etc/shadow", RuleType: "block", RiskLevel: RiskCritical, Description: "System password file", Immutable: true},
		{Pattern: "/etc/passwd", RuleType: "block", RiskLevel: RiskCritical, Description: "System user database", Immutable: true},
		{Pattern: "/etc/sudoers", RuleType: "block", RiskLevel: RiskCritical, Description: "Sudo configuration", Immutable: true},
		{Pattern: "~/.meept/security.db", RuleType: "block", RiskLevel: RiskCritical, Description: "Security database (immutable)", Immutable: true},

		// Blocked paths (immutable) - High
		{Pattern: "~/.aws/*", RuleType: "block", RiskLevel: RiskHigh, Description: "AWS credentials", Immutable: true},
		{Pattern: "~/.config/gcloud/*", RuleType: "block", RiskLevel: RiskHigh, Description: "GCP credentials", Immutable: true},
		{Pattern: "~/.kube/*", RuleType: "block", RiskLevel: RiskHigh, Description: "Kubernetes configuration", Immutable: true},
		{Pattern: "~/.docker/config.json", RuleType: "block", RiskLevel: RiskHigh, Description: "Docker credentials", Immutable: true},
		{Pattern: "~/.azure/*", RuleType: "block", RiskLevel: RiskHigh, Description: "Azure credentials", Immutable: true},
		{Pattern: "~/.config/gh/*", RuleType: "block", RiskLevel: RiskHigh, Description: "GitHub CLI credentials", Immutable: true},
		{Pattern: "~/.netrc", RuleType: "block", RiskLevel: RiskHigh, Description: "Network credentials file", Immutable: true},
		{Pattern: "~/.npmrc", RuleType: "block", RiskLevel: RiskHigh, Description: "npm credentials", Immutable: true},
		{Pattern: "~/.pypirc", RuleType: "block", RiskLevel: RiskHigh, Description: "PyPI credentials", Immutable: true},

		// Blocked paths (mutable)
		{Pattern: "*/.env", RuleType: "block", RiskLevel: RiskHigh, Description: "Environment files with secrets", Immutable: false},
		{Pattern: "*/.env.local", RuleType: "block", RiskLevel: RiskHigh, Description: "Local environment files", Immutable: false},
		{Pattern: "*/.env.production", RuleType: "block", RiskLevel: RiskHigh, Description: "Production environment files", Immutable: false},
		{Pattern: "*/credentials.json", RuleType: "block", RiskLevel: RiskHigh, Description: "Credential files", Immutable: false},
		{Pattern: "*/secrets.yaml", RuleType: "block", RiskLevel: RiskHigh, Description: "Secret configuration files", Immutable: false},
		{Pattern: "*/secrets.yml", RuleType: "block", RiskLevel: RiskHigh, Description: "Secret configuration files", Immutable: false},
		{Pattern: "*/.git/config", RuleType: "block", RiskLevel: RiskMedium, Description: "Git config may contain tokens", Immutable: false},
		{Pattern: "~/.meept/meept.toml", RuleType: "block", RiskLevel: RiskMedium, Description: "Meept configuration (edit manually)", Immutable: false},

		// System paths to block
		{Pattern: "/boot/*", RuleType: "block", RiskLevel: RiskCritical, Description: "Boot files", Immutable: true},
		{Pattern: "/sys/*", RuleType: "block", RiskLevel: RiskCritical, Description: "System files", Immutable: true},
		{Pattern: "/proc/*", RuleType: "block", RiskLevel: RiskHigh, Description: "Process files", Immutable: true},
		{Pattern: "/dev/*", RuleType: "block", RiskLevel: RiskCritical, Description: "Device files", Immutable: true},

		// Allowed paths
		{Pattern: "~/*", RuleType: "allow", RiskLevel: RiskSafe, Description: "User home directory (minus blocked paths)", Immutable: false},
		{Pattern: "/tmp/*", RuleType: "allow", RiskLevel: RiskLow, Description: "Temporary files", Immutable: false},
	}
}

// seedFinancialPatterns returns pre-populated financial patterns (13 rules).
func seedFinancialPatterns() []SeedFinancialPattern {
	return []SeedFinancialPattern{
		{Pattern: `\btransfer\s+(funds?|money|payment)`, PatternType: "regex", Description: "Fund transfer"},
		{Pattern: `\bsend\s+(payment|money|funds?)`, PatternType: "regex", Description: "Payment sending"},
		{Pattern: `\bwire\s+transfer\b`, PatternType: "regex", Description: "Wire transfer"},
		{Pattern: `\b(purchase|buy\s+|sell\s+|trade\s+)`, PatternType: "regex", Description: "Financial transaction"},
		{Pattern: `\b(withdraw|deposit)\b`, PatternType: "regex", Description: "Bank withdrawal/deposit"},
		{Pattern: `\bcredit\s*card\b`, PatternType: "regex", Description: "Credit card operation"},
		{Pattern: `\bdebit\s*card\b`, PatternType: "regex", Description: "Debit card operation"},
		{Pattern: `\bbank\s*account\b`, PatternType: "regex", Description: "Bank account operation"},
		{Pattern: `\b(routing\s*number|swift|iban)\b`, PatternType: "regex", Description: "Banking identifiers"},
		{Pattern: `\b(cryptocurrency|bitcoin|ethereum|wallet\s*address)\b`, PatternType: "regex", Description: "Cryptocurrency operation"},
		{Pattern: `\b(paypal|venmo|zelle|stripe|square)\b`, PatternType: "regex", Description: "Payment platform"},
		{Pattern: `\binvoice\s+payment\b`, PatternType: "regex", Description: "Invoice payment"},
		{Pattern: `\bbilling\s+charge\b`, PatternType: "regex", Description: "Billing charge"},
	}
}

// CountRules returns the total number of seed rules.
func CountRules() int {
	data := SeedRules()
	return len(data.ToolRules) + len(data.CommandPatterns) + len(data.PathRules) + len(data.FinancialPatterns)
}
