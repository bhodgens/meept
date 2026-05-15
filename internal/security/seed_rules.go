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
		{ToolName: ActionFileRead, Action: ActionFileRead, RiskLevel: RiskSafe, Description: "Read a file from the filesystem", RequiresConfirmation: false, Immutable: false},
		{ToolName: ActionFileWrite, Action: ActionFileWrite, RiskLevel: RiskMedium, Description: "Write or overwrite a file on the filesystem", RequiresConfirmation: false, Immutable: false},
		{ToolName: ActionFileDelete, Action: ActionFileDelete, RiskLevel: RiskHigh, Description: "Permanently delete a file from the filesystem", RequiresConfirmation: true, Immutable: false},

		// Shell and network
		{ToolName: "shell", Action: ActionShellExecute, RiskLevel: RiskMedium, Description: "Execute a shell command", RequiresConfirmation: false, Immutable: false},
		{ToolName: "network", Action: "network_request", RiskLevel: RiskLow, Description: "Make an outbound HTTP/HTTPS request", RequiresConfirmation: false, Immutable: false},

		// Messaging and packages
		{ToolName: "send_message", Action: "send_message", RiskLevel: RiskMedium, Description: "Send a message to a user or external service", RequiresConfirmation: false, Immutable: false},
		{ToolName: "install_package", Action: "install_package", RiskLevel: RiskHigh, Description: "Install a software package on the system", RequiresConfirmation: true, Immutable: false},

		// System level
		{ToolName: "system_modify", Action: "system_modify", RiskLevel: RiskCritical, Description: "Modify system-level configuration or settings", RequiresConfirmation: true, Immutable: true},

		// Aliases
		{ToolName: "list_directory", Action: ActionFileRead, RiskLevel: RiskSafe, Description: "List directory contents", RequiresConfirmation: false, Immutable: false},
		{ToolName: "web_fetch", Action: "network_request", RiskLevel: RiskLow, Description: "Fetch content from a URL", RequiresConfirmation: false, Immutable: false},
		{ToolName: "web_search", Action: "network_request", RiskLevel: RiskLow, Description: "Search the web", RequiresConfirmation: false, Immutable: false},
	}
}

// seedCommandPatterns returns pre-populated command patterns (60 rules).
func seedCommandPatterns() []SeedCommandPattern {
	return []SeedCommandPattern{
		// CRITICAL (4) -- Immutable, always blocked
		{Pattern: `\brm\s+-rf\s+/\s*$`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "Recursive delete from root", Immutable: true},
		{Pattern: `\bmkfs\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "Filesystem format", Immutable: true},
		{Pattern: `\bdd\s+if=/dev/(zero|urandom)\s+of=/dev/`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "Disk overwrite", Immutable: true},
		{Pattern: `:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "Fork bomb", Immutable: true},
		{Pattern: `\b(shutdown|reboot|halt|poweroff)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "System power control", Immutable: true},
		{Pattern: `\binit\s+[06]\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "Runlevel change", Immutable: true},
		{Pattern: `\b>\s*/dev/sd[a-z]`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "Direct device write", Immutable: true},
		{Pattern: `\bchmod\s+777\s+/`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategoryDestructive, Description: "World-writable root permissions", Immutable: true},

		// Self-replication detection
		{Pattern: `\bgit\s+clone\b.*meept`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySelfReplication, Description: "Cloning meept repository", Immutable: true},
		{Pattern: `\bpip\s+install\b.*meept`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySelfReplication, Description: "Installing meept package", Immutable: true},
		{Pattern: `\bcp\b.*meept.*meept`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySelfReplication, Description: "Copying meept to create duplicate", Immutable: true},
		{Pattern: `\bdocker\b.*meept`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySelfReplication, Description: "Docker operation involving meept", Immutable: true},

		// Additional critical patterns
		{Pattern: `\bkernelctl\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySystem, Description: "Kernel control", Immutable: true},
		{Pattern: `\bmodprobe\s+-r\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySystem, Description: "Kernel module removal", Immutable: true},
		{Pattern: `\bsysctl\s+-w\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySystem, Description: "Kernel parameter modification", Immutable: true},
		{Pattern: `\b>\s*/etc/passwd\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySystem, Description: "Password file overwrite", Immutable: true},
		{Pattern: `\b>\s*/etc/shadow\b`, PatternType: PatternTypeRegex, RiskLevel: RiskCritical, Category: CategorySystem, Description: "Shadow file overwrite", Immutable: true},

		// HIGH (3) -- Requires confirmation
		{Pattern: `\brm\s+-rf\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryDestructive, Description: "Recursive forced delete", Immutable: false},
		{Pattern: `\brm\s+-r\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryDestructive, Description: "Recursive delete", Immutable: false},
		{Pattern: `\bchmod\s+-R\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "permissions", Description: "Recursive permission change", Immutable: false},
		{Pattern: `\bchown\s+-R\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "permissions", Description: "Recursive ownership change", Immutable: false},
		{Pattern: `\bsystemctl\s+(stop|disable|mask)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "service", Description: "Service disruption", Immutable: false},
		{Pattern: `\bkill\s+-9\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "process", Description: "Force kill process", Immutable: false},
		{Pattern: `\b(iptables|nft|ufw)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "network", Description: "Firewall modification", Immutable: false},
		{Pattern: `\b(deluser|userdel|groupdel)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "user_mgmt", Description: "User/group deletion", Immutable: false},
		{Pattern: `\bcrontab\s+(-e|-r)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "scheduler", Description: "Cron table modification", Immutable: false},
		{Pattern: `\bpip\s+install\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryInstall, Description: "Python package installation", Immutable: false},
		{Pattern: `\bnpm\s+install\s+-g\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryInstall, Description: "Global npm package installation", Immutable: false},
		{Pattern: `\bcargo\s+install\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryInstall, Description: "Rust binary installation", Immutable: false},
		{Pattern: `\b(brew|apt|apt-get|dnf|yum|pacman)\s+install\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryInstall, Description: "System package installation", Immutable: false},
		{Pattern: `\bdocker\s+(run|exec)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "container", Description: "Container execution", Immutable: false},
		{Pattern: `\bcurl\s+.*\|\s*(bash|sh|python|python3)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryCodeExecution, Description: "Pipe-to-shell execution", Immutable: false},
		{Pattern: `\bwget\s+.*\|\s*(bash|sh|python|python3)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryCodeExecution, Description: "Pipe-to-shell execution", Immutable: false},
		{Pattern: `\bsudo\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "privilege", Description: "Privilege escalation", Immutable: false},
		{Pattern: `\bsu\s+-?\s*\w`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "privilege", Description: "User switching", Immutable: false},
		{Pattern: `\bnpm\s+install\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryInstall, Description: "npm package installation", Immutable: false},
		{Pattern: `\blaunchctl\s+(unload|remove)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "service", Description: "macOS service management", Immutable: false},
		{Pattern: `\bpkill\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "process", Description: "Pattern-based process kill", Immutable: false},
		{Pattern: `\bkillall\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: "process", Description: "Kill all processes by name", Immutable: false},
		{Pattern: `\bgit\s+push\s+.*--force\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryVCS, Description: "Force push to git", Immutable: false},
		{Pattern: `\bgit\s+reset\s+--hard\b`, PatternType: PatternTypeRegex, RiskLevel: RiskHigh, Category: CategoryVCS, Description: "Hard git reset", Immutable: false},

		// MEDIUM (2) -- Allowed, logged
		{Pattern: `\b(python|python3)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryCodeExecution, Description: "Python interpreter execution", Immutable: false},
		{Pattern: `\b(node|deno|bun)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryCodeExecution, Description: "JavaScript runtime execution", Immutable: false},
		{Pattern: `\bruby\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryCodeExecution, Description: "Ruby interpreter execution", Immutable: false},
		{Pattern: `\bperl\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryCodeExecution, Description: "Perl interpreter execution", Immutable: false},
		{Pattern: `\bmake\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryBuild, Description: "Build system execution", Immutable: false},
		{Pattern: `\bgit\s+(push|reset|rebase|force-push|checkout\s+--)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryVCS, Description: "Destructive git operations", Immutable: false},
		{Pattern: `\bcargo\s+(build|test|run)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryBuild, Description: "Rust build/test/run", Immutable: false},
		{Pattern: `\b(ssh|scp|rsync)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: "network", Description: "Remote access", Immutable: false},
		{Pattern: `\bpip\s+(list|show|freeze)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: "read_only", Description: "pip read-only commands", Immutable: false},
		{Pattern: `\bnpm\s+(ls|list|info|view)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: "read_only", Description: "npm read-only commands", Immutable: false},
		{Pattern: `\btee\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: "file_write", Description: "Write to file via tee", Immutable: false},
		{Pattern: `\bsed\s+-i\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: "file_write", Description: "In-place file edit", Immutable: false},
		{Pattern: `\bgo\s+(build|run|install)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryBuild, Description: "Go build/run/install", Immutable: false},
		{Pattern: `\bzig\s+(build|run)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskMedium, Category: CategoryBuild, Description: "Zig build/run", Immutable: false},

		// LOW (1) -- Allowed, minimal logging
		{Pattern: `\bgit\s+(status|log|diff|branch|show|remote|tag|stash\s+list)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: CategoryVCS, Description: "Read-only git operations", Immutable: false},
		{Pattern: `\b(ls|dir|pwd|whoami|hostname|uname|date|uptime|id)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: "system_info", Description: "System information commands", Immutable: false},
		{Pattern: `\b(cat|head|tail|less|more|wc|file|stat)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: "file_read", Description: "File reading commands", Immutable: false},
		{Pattern: `\b(grep|rg|find|fd|locate|which|whereis|type)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: "search", Description: "File/command search", Immutable: false},
		{Pattern: `\b(echo|printf)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: "output", Description: "Output commands", Immutable: false},
		{Pattern: `\b(env|printenv|set)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: "system_info", Description: "Environment inspection", Immutable: false},
		{Pattern: `\b(ps|top|htop|df|du|free|vmstat|iostat)\b`, PatternType: PatternTypeRegex, RiskLevel: RiskLow, Category: "monitoring", Description: "System monitoring commands", Immutable: false},
	}
}

// seedPathRules returns pre-populated path rules (29 rules).
func seedPathRules() []SeedPathRule {
	return []SeedPathRule{
		// Blocked paths (immutable) - Critical
		{Pattern: "~/.ssh/*", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "SSH keys and configuration", Immutable: true},
		{Pattern: "~/.gnupg/*", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "GPG keys and configuration", Immutable: true},
		{Pattern: "/etc/shadow", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "System password file", Immutable: true},
		{Pattern: "/etc/passwd", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "System user database", Immutable: true},
		{Pattern: "/etc/sudoers", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "Sudo configuration", Immutable: true},
		{Pattern: "~/.meept/security.db", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "Security database (immutable)", Immutable: true},

		// Blocked paths (immutable) - High
		{Pattern: "~/.aws/*", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "AWS credentials", Immutable: true},
		{Pattern: "~/.config/gcloud/*", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "GCP credentials", Immutable: true},
		{Pattern: "~/.kube/*", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Kubernetes configuration", Immutable: true},
		{Pattern: "~/.docker/config.json", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Docker credentials", Immutable: true},
		{Pattern: "~/.azure/*", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Azure credentials", Immutable: true},
		{Pattern: "~/.config/gh/*", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "GitHub CLI credentials", Immutable: true},
		{Pattern: "~/.netrc", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Network credentials file", Immutable: true},
		{Pattern: "~/.npmrc", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "npm credentials", Immutable: true},
		{Pattern: "~/.pypirc", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "PyPI credentials", Immutable: true},

		// Blocked paths (mutable)
		{Pattern: "*/.env", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Environment files with secrets", Immutable: false},
		{Pattern: "*/.env.local", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Local environment files", Immutable: false},
		{Pattern: "*/.env.production", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Production environment files", Immutable: false},
		{Pattern: "*/credentials.json", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Credential files", Immutable: false},
		{Pattern: "*/secrets.yaml", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Secret configuration files", Immutable: false},
		{Pattern: "*/secrets.yml", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Secret configuration files", Immutable: false},
		{Pattern: "*/.git/config", RuleType: DecisionBlock, RiskLevel: RiskMedium, Description: "Git config may contain tokens", Immutable: false},
		{Pattern: "~/.meept/meept.toml", RuleType: DecisionBlock, RiskLevel: RiskMedium, Description: "Meept configuration (edit manually)", Immutable: false},

		// System paths to block
		{Pattern: "/boot/*", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "Boot files", Immutable: true},
		{Pattern: "/sys/*", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "System files", Immutable: true},
		{Pattern: "/proc/*", RuleType: DecisionBlock, RiskLevel: RiskHigh, Description: "Process files", Immutable: true},
		{Pattern: "/dev/*", RuleType: DecisionBlock, RiskLevel: RiskCritical, Description: "Device files", Immutable: true},

		// Allowed paths
		{Pattern: "~/*", RuleType: DecisionAllow, RiskLevel: RiskSafe, Description: "User home directory (minus blocked paths)", Immutable: false},
		{Pattern: "/tmp/*", RuleType: DecisionAllow, RiskLevel: RiskLow, Description: "Temporary files", Immutable: false},
	}
}

// seedFinancialPatterns returns pre-populated financial patterns (13 rules).
func seedFinancialPatterns() []SeedFinancialPattern {
	return []SeedFinancialPattern{
		{Pattern: `\btransfer\s+(funds?|money|payment)`, PatternType: PatternTypeRegex, Description: "Fund transfer"},
		{Pattern: `\bsend\s+(payment|money|funds?)`, PatternType: PatternTypeRegex, Description: "Payment sending"},
		{Pattern: `\bwire\s+transfer\b`, PatternType: PatternTypeRegex, Description: "Wire transfer"},
		{Pattern: `\b(purchase|buy\s+|sell\s+|trade\s+)`, PatternType: PatternTypeRegex, Description: "Financial transaction"},
		{Pattern: `\b(withdraw|deposit)\b`, PatternType: PatternTypeRegex, Description: "Bank withdrawal/deposit"},
		{Pattern: `\bcredit\s*card\b`, PatternType: PatternTypeRegex, Description: "Credit card operation"},
		{Pattern: `\bdebit\s*card\b`, PatternType: PatternTypeRegex, Description: "Debit card operation"},
		{Pattern: `\bbank\s*account\b`, PatternType: PatternTypeRegex, Description: "Bank account operation"},
		{Pattern: `\b(routing\s*number|swift|iban)\b`, PatternType: PatternTypeRegex, Description: "Banking identifiers"},
		{Pattern: `\b(cryptocurrency|bitcoin|ethereum|wallet\s*address)\b`, PatternType: PatternTypeRegex, Description: "Cryptocurrency operation"},
		{Pattern: `\b(paypal|venmo|zelle|stripe|square)\b`, PatternType: PatternTypeRegex, Description: "Payment platform"},
		{Pattern: `\binvoice\s+payment\b`, PatternType: PatternTypeRegex, Description: "Invoice payment"},
		{Pattern: `\bbilling\s+charge\b`, PatternType: PatternTypeRegex, Description: "Billing charge"},
	}
}

// CountRules returns the total number of seed rules.
func CountRules() int {
	data := SeedRules()
	return len(data.ToolRules) + len(data.CommandPatterns) + len(data.PathRules) + len(data.FinancialPatterns)
}
