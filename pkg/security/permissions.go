// Package security provides fast permission checking for agent actions.
package security

import (
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

// RiskLevel represents the severity of an action.
type RiskLevel int

const (
	RiskSafe RiskLevel = iota
	RiskLow
	RiskMedium
	RiskHigh
	RiskCritical
)

func (r RiskLevel) String() string {
	switch r {
	case RiskSafe:
		return "SAFE"
	case RiskLow:
		return "LOW"
	case RiskMedium:
		return "MEDIUM"
	case RiskHigh:
		return "HIGH"
	case RiskCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// ActionRule describes permissions for an action type.
type ActionRule struct {
	Action               string
	RiskLevel            RiskLevel
	RequiresConfirmation bool
}

// Builtin action rules.
var BuiltinRules = map[string]ActionRule{
	"file_read":       {Action: "file_read", RiskLevel: RiskSafe, RequiresConfirmation: false},
	"file_write":      {Action: "file_write", RiskLevel: RiskMedium, RequiresConfirmation: false},
	"file_delete":     {Action: "file_delete", RiskLevel: RiskHigh, RequiresConfirmation: true},
	"shell_execute":   {Action: "shell_execute", RiskLevel: RiskMedium, RequiresConfirmation: false},
	"network_request": {Action: "network_request", RiskLevel: RiskLow, RequiresConfirmation: false},
	"send_message":    {Action: "send_message", RiskLevel: RiskMedium, RequiresConfirmation: false},
	"install_package": {Action: "install_package", RiskLevel: RiskHigh, RequiresConfirmation: true},
	"system_modify":   {Action: "system_modify", RiskLevel: RiskCritical, RequiresConfirmation: true},
}

// Dangerous command patterns that elevate shell_execute to HIGH risk.
var dangerousCommandsRE = regexp.MustCompile(
	`(?i)\b(rm\s+-rf|mkfs|dd\s+if=|chmod\s+-R|chown\s+-R|shutdown|reboot` +
		`|init\s+[06]|systemctl\s+(stop|disable|mask)|kill\s+-9` +
		`|iptables|nft|deluser|userdel|groupdel)\b`,
)

// Financial operation patterns.
var financialPatternsRE = regexp.MustCompile(
	`(?i)\b(transfer\s+(funds?|money|payment)|send\s+(payment|money|funds?)` +
		`|wire\s+transfer|purchase|buy|sell|trade|withdraw` +
		`|credit\s*card|bank\s*account|routing\s*number` +
		`|cryptocurrency|bitcoin|ethereum|wallet\s*address)\b`,
)

// Config holds permission checker configuration.
type Config struct {
	AllowedPaths              []string
	BlockedPaths              []string
	BlockFinancial            bool
	RequireConfirmationHigh   bool
	RequireConfirmationCritical bool
}

// PermissionChecker provides fast permission checking.
type PermissionChecker struct {
	config       Config
	allowedGlobs []string
	blockedGlobs []string
	homeDir      string
}

// NewPermissionChecker creates a new permission checker.
func NewPermissionChecker(cfg Config) *PermissionChecker {
	pc := &PermissionChecker{
		config:       cfg,
		allowedGlobs: make([]string, 0, len(cfg.AllowedPaths)),
		blockedGlobs: make([]string, 0, len(cfg.BlockedPaths)),
	}

	// Get home directory for tilde expansion
	if u, err := user.Current(); err == nil {
		pc.homeDir = u.HomeDir
	}

	// Pre-expand paths
	for _, p := range cfg.AllowedPaths {
		pc.allowedGlobs = append(pc.allowedGlobs, pc.expandPath(p))
	}
	for _, p := range cfg.BlockedPaths {
		pc.blockedGlobs = append(pc.blockedGlobs, pc.expandPath(p))
	}

	return pc
}

func (pc *PermissionChecker) expandPath(path string) string {
	if strings.HasPrefix(path, "~") && pc.homeDir != "" {
		path = pc.homeDir + path[1:]
	}
	// Clean the path
	return filepath.Clean(path)
}

// CheckPath returns true if the path is allowed.
func (pc *PermissionChecker) CheckPath(path string) bool {
	allowed, _ := pc.checkPathWithReason(path)
	return allowed
}

// CheckPathWithReason returns (allowed, reason).
func (pc *PermissionChecker) checkPathWithReason(pathStr string) (bool, string) {
	resolved := pc.expandPath(pathStr)
	if absPath, err := filepath.Abs(resolved); err == nil {
		resolved = absPath
	}

	// Block list takes precedence
	for _, pattern := range pc.blockedGlobs {
		if matched, _ := filepath.Match(pattern, resolved); matched {
			return false, "Path matches blocked pattern: " + pattern
		}
		// Also check if path starts with blocked directory
		if strings.HasPrefix(resolved, pattern) {
			return false, "Path is within blocked directory: " + pattern
		}
	}

	// If there's an allow list, path must match at least one
	if len(pc.allowedGlobs) > 0 {
		for _, pattern := range pc.allowedGlobs {
			if matched, _ := filepath.Match(pattern, resolved); matched {
				return true, "Path is within allowed paths"
			}
			// Check if path starts with allowed directory
			if strings.HasPrefix(resolved, pattern) {
				return true, "Path is within allowed directory"
			}
		}
		return false, "Path does not match any allowed path pattern"
	}

	return true, "No path restrictions configured"
}

// EvaluateShellRisk returns the risk level for a shell command.
func EvaluateShellRisk(command string) RiskLevel {
	if dangerousCommandsRE.MatchString(command) {
		return RiskHigh
	}
	return RiskMedium
}

// IsFinancial returns true if the text contains financial operation patterns.
func IsFinancial(text string) bool {
	return financialPatternsRE.MatchString(text)
}

// CheckResult is the result of a permission check.
type CheckResult struct {
	Allowed       bool
	Reason        string
	EffectiveRisk RiskLevel
	NeedsConfirm  bool
}

// CheckPermission checks if an action is permitted.
func (pc *PermissionChecker) CheckPermission(action string, details map[string]string) CheckResult {
	// Look up base rule
	rule, ok := BuiltinRules[action]
	if !ok {
		return CheckResult{
			Allowed: false,
			Reason:  "Unknown action: " + action,
		}
	}

	effectiveRisk := rule.RiskLevel

	// Financial gate
	if pc.config.BlockFinancial {
		for _, v := range details {
			if IsFinancial(v) {
				return CheckResult{
					Allowed: false,
					Reason:  "Financial operations are blocked by policy",
				}
			}
		}
	}

	// Path-based checks for file actions
	if action == "file_read" || action == "file_write" || action == "file_delete" {
		if path, ok := details["path"]; ok && path != "" {
			allowed, reason := pc.checkPathWithReason(path)
			if !allowed {
				return CheckResult{
					Allowed: false,
					Reason:  reason,
				}
			}
		}
	}

	// Shell command risk elevation
	if action == "shell_execute" {
		if command, ok := details["command"]; ok {
			effectiveRisk = EvaluateShellRisk(command)
		}
	}

	// Confirmation gating
	needsConfirm := false
	if effectiveRisk >= RiskHigh && pc.config.RequireConfirmationHigh {
		needsConfirm = true
	}
	if effectiveRisk >= RiskCritical && pc.config.RequireConfirmationCritical {
		needsConfirm = true
	}

	if needsConfirm {
		return CheckResult{
			Allowed:       false,
			Reason:        "Action requires user confirmation",
			EffectiveRisk: effectiveRisk,
			NeedsConfirm:  true,
		}
	}

	return CheckResult{
		Allowed:       true,
		Reason:        "Permitted",
		EffectiveRisk: effectiveRisk,
	}
}
