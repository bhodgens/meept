// Package security provides fast permission checking for agent actions.
package security

import (
	"maps"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

// RiskLevel represents the severity of an action.
type RiskLevel int

//go:generate go run golang.org/x/tools/cmd/stringer -type=RiskLevel -linecomment

const (
	RiskSafe     RiskLevel = iota // SAFE
	RiskLow                       // LOW
	RiskMedium                    // MEDIUM
	RiskHigh                      // HIGH
	RiskCritical                  // CRITICAL
)

// ActionRule describes permissions for an action type.
type ActionRule struct {
	Action               string
	RiskLevel            RiskLevel
	RequiresConfirmation bool
}

// Builtin action rules.
var BuiltinRules = map[string]ActionRule{
	// File operations
	"file_read":   {Action: "file_read", RiskLevel: RiskSafe, RequiresConfirmation: false},
	"file_write":  {Action: "file_write", RiskLevel: RiskMedium, RequiresConfirmation: false},
	"file_delete": {Action: "file_delete", RiskLevel: RiskHigh, RequiresConfirmation: true},

	// Shell/system operations
	"shell_execute":   {Action: "shell_execute", RiskLevel: RiskMedium, RequiresConfirmation: false},
	"install_package": {Action: "install_package", RiskLevel: RiskHigh, RequiresConfirmation: true},
	"system_modify":   {Action: "system_modify", RiskLevel: RiskCritical, RequiresConfirmation: true},

	// Network operations
	"network_request": {Action: "network_request", RiskLevel: RiskLow, RequiresConfirmation: false},
	"send_message":    {Action: "send_message", RiskLevel: RiskMedium, RequiresConfirmation: false},

	// Memory operations
	"memory_read":  {Action: "memory_read", RiskLevel: RiskSafe, RequiresConfirmation: false},
	"memory_write": {Action: "memory_write", RiskLevel: RiskLow, RequiresConfirmation: false},

	// Platform introspection (always safe - read-only)
	"platform_read": {Action: "platform_read", RiskLevel: RiskSafe, RequiresConfirmation: false},

	// Task management
	"task_read":  {Action: "task_read", RiskLevel: RiskSafe, RequiresConfirmation: false},
	"task_write": {Action: "task_write", RiskLevel: RiskLow, RequiresConfirmation: false},

	// Agent operations
	"agent_delegate": {Action: "agent_delegate", RiskLevel: RiskLow, RequiresConfirmation: false},
	"skill_execute":  {Action: "skill_execute", RiskLevel: RiskMedium, RequiresConfirmation: false},

	// Voice sidecar (voice-sidecar leaf 14): one action row for the
	// employee-callable outbound dial. Unknown actions DENY by default, so
	// this row is MANDATORY - without it every voice_call is denied
	// "Unknown action" regardless of the employee's tools_allowed grant.
	// MEDIUM: it initiates a real-world phone call (the dialer's own
	// policy gates - allowlist, registry cap, effects idempotency, audit
	// row - run before any HTTP, so the risk here is the outbound action
	// itself, on par with send_message + network_request).
	"voice_call": {Action: "voice_call", RiskLevel: RiskMedium, RequiresConfirmation: false},

	// Media generation (network + file write)
	"generate_image": {Action: "generate_image", RiskLevel: RiskMedium, RequiresConfirmation: false},
	"generate_video": {Action: "generate_video", RiskLevel: RiskMedium, RequiresConfirmation: false},
}

// Dangerous command patterns that elevate shell_execute to HIGH risk.
var dangerousCommandsRE = regexp.MustCompile(
	`(?i)\b(rm\s+-rf|mkfs|dd\s+if=|chmod\s+-R|chown\s+-R|shutdown|reboot` +
		`|init\s+[06]|systemctl\s+(stop|disable|mask)|kill\s+-9` +
		`|iptables|nft|deluser|userdel|groupdel)\b`,
)

// Financial operation patterns.
var financialPatternsRE = regexp.MustCompile(
	`(?i)\b(transfer\s*(funds?|money|payment)|send\s*(payment|money|funds?)` +
		`|wire\s*transfer|purchase|buy|sell|trade|withdraw` +
		`|credit\s*card|bank\s*account|routing\s*number` +
		`|cryptocurrency|bitcoin|ethereum|wallet\s*address)\b`,
)

// Config holds permission checker configuration.
type Config struct {
	AllowedPaths                []string
	BlockedPaths                []string
	BlockFinancial              bool
	RequireConfirmationHigh     bool
	RequireConfirmationCritical bool
}

// PermissionChecker provides fast permission checking.
type PermissionChecker struct {
	config       Config
	allowedGlobs []string
	blockedGlobs []string
	homeDir      string

	// fenceMu guards fence. Separate from the main config fields (which
	// are immutable after construction) so SetFence can happen concurrently
	// with checks. The fence's own Check calls run outside the lock (the
	// same collect-under-lock/release-then-operate pattern as preExecMu).
	fenceMu sync.RWMutex
	fence   FenceStage

	// preExecMu guards preExecCheckers. Separate from the main config
	// fields (which are immutable after construction) so registration
	// can happen concurrently with reads. Per CLAUDE.md mutex-scope:
	// the lock is held only to snapshot the map, not during the
	// checker's Check call (which is in-memory and lock-free).
	preExecMu       sync.RWMutex
	preExecCheckers map[string]PreExecChecker
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

	// Pre-expand paths: store BOTH resolved and raw forms so that paths
	// which cannot be symlink-resolved at check time (e.g. non-existent
	// files under autofs mounts) still match their configured prefix.
	for _, p := range cfg.AllowedPaths {
		pc.allowedGlobs = append(pc.allowedGlobs, pc.expandPathAll(p)...)
	}
	for _, p := range cfg.BlockedPaths {
		pc.blockedGlobs = append(pc.blockedGlobs, pc.expandPathAll(p)...)
	}

	return pc
}

// expandPathAll returns the resolved and raw cleaned forms of a path.
// Both are needed because EvalSymlinks may succeed for the configured
// pattern but fail for a requested path under it (common on macOS where
// /home is an autofs mount). Storing both forms ensures the prefix
// check succeeds regardless of which side resolves.
func (pc *PermissionChecker) expandPathAll(path string) []string {
	if strings.HasPrefix(path, "~") && pc.homeDir != "" {
		path = pc.homeDir + path[1:]
	}
	cleaned := filepath.Clean(path)
	forms := []string{cleaned}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil && resolved != cleaned {
		forms = append(forms, resolved)
	}
	return forms
}

// SetPreExecChecker registers a PreExecChecker for the given agent/employee
// ID. The checker's Check method is invoked at the top of CheckPermission
// (before the financial/path/risk pipeline) whenever details["agent_id"]
// matches agentID. An empty agentID is a no-op (nil guard). Passing a nil
// checker clears the registration.
//
// Safe for concurrent use. The checker's Check call happens outside the
// preExecMu lock (collect-under-lock, release, then operate pattern per
// CLAUDE.md mutex-scope rule).
func (pc *PermissionChecker) SetPreExecChecker(agentID string, checker PreExecChecker) {
	if agentID == "" {
		return
	}
	pc.preExecMu.Lock()
	defer pc.preExecMu.Unlock()
	if pc.preExecCheckers == nil {
		pc.preExecCheckers = make(map[string]PreExecChecker)
	}
	if checker != nil {
		pc.preExecCheckers[agentID] = checker
	}
}

// RemovePreExecChecker unregisters the PreExecChecker for the given agent
// ID. An empty agentID is a no-op. Safe to call when no checker is
// registered (idempotent).
func (pc *PermissionChecker) RemovePreExecChecker(agentID string) {
	if agentID == "" {
		return
	}
	pc.preExecMu.Lock()
	defer pc.preExecMu.Unlock()
	if pc.preExecCheckers != nil {
		delete(pc.preExecCheckers, agentID)
	}
}

// snapshotPreExecCheckers returns the current checker map under a read lock.
// The returned map is a shallow copy so callers can iterate without holding
// the lock. Returns nil when no checkers are registered.
func (pc *PermissionChecker) snapshotPreExecCheckers() map[string]PreExecChecker {
	pc.preExecMu.RLock()
	defer pc.preExecMu.RUnlock()
	if len(pc.preExecCheckers) == 0 {
		return nil
	}
	return maps.Clone(pc.preExecCheckers)
}

// ConfigSnapshot returns the checker's immutable path/risk configuration
// (path allow/block globs, financial gate, confirmation requirements).
// Read-only by convention: callers must not mutate the returned slices.
// Used to derive per-session clones that carry their own fence stage.
func (pc *PermissionChecker) ConfigSnapshot() Config {
	return pc.config
}

// SetFence installs the project-boundary fence stage consulted by
// CheckPermissionForAgent for file and shell actions. Typed-nil guarded:
// a nil FenceStage (including a typed-nil pointer wrapped in the
// interface) clears the fence instead of storing a non-nil interface
// holding nil — fenced behavior stays exactly the legacy pipeline.
//
// Safe for concurrent use. The fence's Check calls happen outside the
// fenceMu lock (same mutex-scope rule as SetPreExecChecker).
func (pc *PermissionChecker) SetFence(fence FenceStage) {
	pc.fenceMu.Lock()
	defer pc.fenceMu.Unlock()
	if fence == nil {
		pc.fence = nil
		return
	}
	if v := reflectValueOf(fence); v.Kind() == reflect.Ptr && v.IsNil() {
		pc.fence = nil
		return
	}
	pc.fence = fence
}

// snapshotFence returns the current fence stage (nil when none is set).
func (pc *PermissionChecker) snapshotFence() FenceStage {
	pc.fenceMu.RLock()
	defer pc.fenceMu.RUnlock()
	return pc.fence
}

// reflectValueOf reflects ValueOf(fence), localized so the reflect import
// stays next to its only use.
func reflectValueOf(v FenceStage) reflect.Value { return reflect.ValueOf(v) }

// CheckPath returns true if the path is allowed.
func (pc *PermissionChecker) CheckPath(path string) bool {
	allowed, _ := pc.checkPathWithReason(path)
	return allowed
}

// CheckPathFence consults ONLY the fence stage for path under op
// (agent-groups branch-01 leaf 02 T4): it is the revert-path seam — a
// journal revert is a file write that bypasses CheckPermissionForAgent's
// action pipeline, so the caller routes it through here to get the same
// rule_source=fence verdict any write would receive. A nil fence returns
// nil (allowed), matching the stage's legacy-skip semantics.
func (pc *PermissionChecker) CheckPathFence(path, op string) error {
	if fence := pc.snapshotFence(); fence != nil {
		return fence.CheckPath(path, op)
	}
	return nil
}

// CheckPathWithReason returns (allowed, reason).
func (pc *PermissionChecker) checkPathWithReason(pathStr string) (ok bool, result string) {
	// Use expandPathAll to get BOTH raw and resolved forms.
	// This is needed because EvalSymlinks may succeed for the configured
	// allow pattern but fail for a requested path under it (common on
	// macOS where /home is an autofs mount). Checking against both forms
	// ensures the prefix match succeeds regardless of which side resolves.
	pathForms := pc.expandPathAll(pathStr)
	// Also produce an Abs form for each, since configured globs are abs.
	for i, p := range pathForms {
		if absPath, err := filepath.Abs(p); err == nil {
			pathForms[i] = absPath
		}
	}

	// Block list takes precedence
	for _, pattern := range pc.blockedGlobs {
		for _, p := range pathForms {
			if pc.matchGlobPattern(pattern, p) {
				return false, "Path matches blocked pattern: " + pattern
			}
		}
	}

	// If there's an allow list, path must match at least one
	if len(pc.allowedGlobs) > 0 {
		for _, pattern := range pc.allowedGlobs {
			for _, p := range pathForms {
				if pc.matchGlobPattern(pattern, p) {
					return true, "Path is within allowed paths"
				}
			}
		}
		return false, "Path does not match any allowed path pattern"
	}

	return true, "No path restrictions configured"
}

// matchGlobPattern handles both exact globs and directory prefixes.
// Patterns ending with /* or /** are treated as directory prefixes.
func (pc *PermissionChecker) matchGlobPattern(pattern, path string) bool {
	// Handle trailing /* or /** as directory prefix match
	if strings.HasSuffix(pattern, "/*") || strings.HasSuffix(pattern, "/**") {
		// Strip the trailing /* or /**
		dirPrefix := strings.TrimSuffix(strings.TrimSuffix(pattern, "**"), "*")
		dirPrefix = strings.TrimSuffix(dirPrefix, "/")

		// Check if path equals the directory or is under it
		if path == dirPrefix {
			return true
		}
		// Check if path is under this directory
		if strings.HasPrefix(path, dirPrefix+"/") {
			return true
		}
		return false
	}

	// Try exact filepath.Match for non-prefix patterns
	if matched, _ := filepath.Match(pattern, path); matched {
		return true
	}

	// Also check exact path match (for patterns without wildcards)
	if pattern == path {
		return true
	}

	// Check if path is under the pattern directory (for directory patterns without *)
	if strings.HasPrefix(path, pattern+"/") {
		return true
	}

	// A bare root pattern ("/" after cleaning) fences nothing by glob shape:
	// the prefix rule above would need path == "/" + name, and filepath.Match
	// never matches across the separator. Every path is under the root, so
	// it always allows.
	if pattern == "/" {
		return true
	}

	return false
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
	Allowed bool
	Reason  string
	// RuleSource stamps which pipeline stage produced this result (leaf 02
	// of agent-groups branch-01, frozen vocabulary). Every return site in
	// this package stamps it; group_deny/group_allow fire once leaf 04
	// feeds group rules in.
	RuleSource    string
	EffectiveRisk RiskLevel
	NeedsConfirm  bool
}

// checkRiskResult converts a resolved risk level into a CheckResult,
// applying the financial gate and the confirmation gating shared with
// CheckPermissionForAgent.
func (pc *PermissionChecker) checkRiskResult(details map[string]string, risk RiskLevel) CheckResult {
	for _, v := range details {
		if pc.config.BlockFinancial && IsFinancial(v) {
			return CheckResult{
				Allowed:    false,
				Reason:     "Financial operations are blocked by policy",
				RuleSource: "financial",
			}
		}
	}

	needsConfirm := risk >= RiskHigh && pc.config.RequireConfirmationHigh
	if risk >= RiskCritical && pc.config.RequireConfirmationCritical {
		needsConfirm = true
	}
	if needsConfirm {
		return CheckResult{
			Allowed:       false,
			Reason:        "Action requires user confirmation",
			RuleSource:    "confirmation_gate",
			EffectiveRisk: risk,
			NeedsConfirm:  true,
		}
	}

	return CheckResult{
		Allowed:       true,
		Reason:        "Permitted",
		RuleSource:    "base_rule",
		EffectiveRisk: risk,
	}
}

// CheckPermission checks if an action is permitted.
func (pc *PermissionChecker) CheckPermission(action string, details map[string]string) CheckResult {
	return pc.CheckPermissionForAgent(action, details, "")
}

// CheckPermissionForAgent checks if an action is permitted for a specific
// agent/employee. When agentID is non-empty and a PreExecChecker is registered
// for that ID, the constitution gate runs before the financial/path/risk
// pipeline.
//
// E6: This new method provides backward-compatible agentID support. The
// existing CheckPermission delegates to this with agentID="" which skips
// the employee pre-exec stage entirely (no behavior change for existing
// callers). Callers that have an agentID should use this method directly.
//
// When details["agent_id"] is already set, it takes precedence over the
// agentID parameter — this allows the existing details-based path to work
// unchanged while new callers can pass agentID explicitly.
func (pc *PermissionChecker) CheckPermissionForAgent(action string, details map[string]string, agentID string) CheckResult {
	// Stage 0: Employee pre-exec check (Checkpoint 1). Runs BEFORE the
	// financial/path/risk pipeline so the constitution gate has the
	// first say. Only fires when details["agent_id"] is non-empty and
	// a checker is registered for that ID. Non-employee agents (empty
	// agent_id or no registered checker) skip this stage entirely — no
	// behavior change for existing agents.
	//
	// The tool name is read from details["tool_name"] (populated by
	// Executor.checkPermission) so that tools_allowed / tools_forbidden
	// entries that use tool names (e.g. "git_push", "file_delete") match
	// correctly rather than silently missing.
	//
	// E6: agentID from the parameter is used as fallback when
	// details["agent_id"] is empty. This preserves backward compat
	// (CheckPermission passes "" → skips) while allowing new callers
	// to pass agentID explicitly.
	if details != nil {
		effectiveAgentID := details["agent_id"]
		if effectiveAgentID == "" {
			effectiveAgentID = agentID
		}
		if effectiveAgentID != "" {
			if checkers := pc.snapshotPreExecCheckers(); checkers != nil {
				if checker, ok := checkers[effectiveAgentID]; ok {
					toolName := details["tool_name"]
					preDecision := checker.Check(action, toolName, details)
					if !preDecision.Allowed {
						reason := preDecision.Reason
						needsConfirm := false
						if preDecision.RequiresPlan {
							needsConfirm = true
							reason = "requires plan signoff: " + reason
						}
						return CheckResult{
							Allowed:      false,
							Reason:       reason,
							RuleSource:   "employee_pre_exec",
							NeedsConfirm: needsConfirm,
						}
					}
				}
			}
		}
	}

	// Look up base rule. cua-driver (computer-use) tools are matched by
	// registered-name prefix first: observation actions run at LOW, input
	// injection and any unknown cua-driver action classify HIGH
	// (fail-closed). Everything else resolves through BuiltinRules, which
	// denies unknown actions outright.
	if risk, ok := ComputerUseRule(action); ok {
		return pc.checkRiskResult(details, risk)
	}
	if risk, ok := ACPAgentRule(action, details); ok {
		return pc.checkRiskResult(details, risk)
	}

	rule, ok := BuiltinRules[action]
	if !ok {
		return CheckResult{
			Allowed:    false,
			Reason:     "Unknown action: " + action,
			RuleSource: "unknown_action",
		}
	}

	effectiveRisk := rule.RiskLevel

	// Financial gate
	if pc.config.BlockFinancial {
		for _, v := range details {
			if IsFinancial(v) {
				return CheckResult{
					Allowed:    false,
					Reason:     "Financial operations are blocked by policy",
					RuleSource: "financial",
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
					Allowed:    false,
					Reason:     reason,
					RuleSource: "base_rule",
				}
			}
		}
	}

	// Fence stage (agent-groups leaf 01): the project-boundary check runs
	// AFTER the path globs (a glob deny short-circuits above and the fence
	// is never consulted) and BEFORE confirmation gating (a fence deny wins
	// over the confirmation result). A nil fence skips this stage entirely
	// — byte-identical legacy behavior.
	if fence := pc.snapshotFence(); fence != nil {
		var fenceErr error
		switch action {
		case "file_read", "file_write", "file_delete":
			if path := details["path"]; path != "" {
				op := "read"
				switch action {
				case "file_write":
					op = "write"
				case "file_delete":
					op = "delete"
				}
				fenceErr = fence.CheckPath(path, op)
			}
		case "shell_execute":
			workDir := details["workdir"]
			if workDir == "" {
				workDir = "."
			}
			// The workdir boundary is validated as a path ("exec" op,
			// mirroring the real fence checker's own CheckCommand
			// internals) alongside the command check: a FenceStage
			// implementation may enforce either surface.
			if err := fence.CheckPath(workDir, "exec"); err != nil {
				fenceErr = err
			} else {
				fenceErr = fence.CheckCommand(details["command"], workDir)
			}
		}
		if fenceErr != nil {
			return CheckResult{
				Allowed:    false,
				Reason:     fenceErr.Error(),
				RuleSource: "fence",
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
	needsConfirm := effectiveRisk >= RiskHigh && pc.config.RequireConfirmationHigh

	if effectiveRisk >= RiskCritical && pc.config.RequireConfirmationCritical {
		needsConfirm = true
	}

	if needsConfirm {
		return CheckResult{
			Allowed:       false,
			Reason:        "Action requires user confirmation",
			RuleSource:    "confirmation_gate",
			EffectiveRisk: effectiveRisk,
			NeedsConfirm:  true,
		}
	}

	return CheckResult{
		Allowed:       true,
		Reason:        "Permitted",
		RuleSource:    "base_rule",
		EffectiveRisk: effectiveRisk,
	}
}
