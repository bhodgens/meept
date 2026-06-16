package security

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/pathutil"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite3 driver registration
)

// compiledPattern holds a pre-compiled regex pattern with metadata.
type compiledPattern struct {
	Pattern     *regexp.Regexp
	RiskLevel   RiskLevel
	Category    string
	Description string
	Immutable   bool
}

// Engine is the SQLite-backed security decision engine.
type Engine struct {
	mu                sync.RWMutex
	db                *sqlx.DB
	config            *config.SecurityConfig
	homeDir           string
	compiledCommands  []compiledPattern
	compiledFinancial []*regexp.Regexp
	logger            *slog.Logger
	fenceChecker      *FenceChecker
}

// NewEngine creates a new security engine with the given database path.
func NewEngine(dbPath string, cfg *config.SecurityConfig, logger *slog.Logger) (*Engine, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Expand home directory
	if strings.HasPrefix(dbPath, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(homeDir, dbPath[2:])
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	// SQLite only supports one writer at a time. Constraining the pool to a
	// single connection prevents SQLITE_BUSY from concurrent writes routed
	// through different C-level sqlite3 connections in the pool.
	sqlDB.SetMaxOpenConns(1)
	db := sqlx.NewDb(sqlDB, "sqlite")

	// Get home directory for path expansion
	homeDir := ""
	if u, err := user.Current(); err == nil {
		homeDir = u.HomeDir
	}

	e := &Engine{
		db:      db,
		config:  cfg,
		homeDir: homeDir,
		logger:  logger,
	}

	if err := e.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return e, nil
}

// initialize creates the schema and seeds default rules.
func (e *Engine) initialize() error {
	if err := e.createSchema(); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	if err := e.seedDefaults(); err != nil {
		return fmt.Errorf("failed to seed defaults: %w", err)
	}

	if err := e.compilePatterns(); err != nil {
		return fmt.Errorf("failed to compile patterns: %w", err)
	}

	e.logger.Info("SecurityEngine initialized", "db", e.db)
	return nil
}

// SetFenceChecker sets the fence checker for path boundary enforcement.
// Pass nil to disable fencing for this session. The typed-nil guard
// prevents accidental interface-nil assignment.
func (e *Engine) SetFenceChecker(fc *FenceChecker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if fc != nil {
		e.fenceChecker = fc
	} else {
		e.fenceChecker = nil
	}
}

// createSchema creates the database tables.
func (e *Engine) createSchema() error {
	_, err := e.db.Exec(schemaSQL)
	return err
}

// seedDefaults inserts default rules if not already present.
func (e *Engine) seedDefaults() error {
	rules := SeedRules()

	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Seed tool rules
	for _, r := range rules.ToolRules {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO tool_rules
			(tool_name, action, risk_level, description, requires_confirmation, immutable)
			VALUES (?, ?, ?, ?, ?, ?)`,
			r.ToolName, r.Action, r.RiskLevel, r.Description,
			boolToInt(r.RequiresConfirmation), boolToInt(r.Immutable))
		if err != nil {
			return err
		}
	}

	// Seed command patterns
	for _, p := range rules.CommandPatterns {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO command_patterns
			(pattern, pattern_type, risk_level, category, description, immutable)
			VALUES (?, ?, ?, ?, ?, ?)`,
			p.Pattern, p.PatternType, p.RiskLevel, p.Category,
			p.Description, boolToInt(p.Immutable))
		if err != nil {
			return err
		}
	}

	// Seed path rules
	for _, r := range rules.PathRules {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO path_rules
			(pattern, rule_type, risk_level, description, immutable)
			VALUES (?, ?, ?, ?, ?)`,
			r.Pattern, r.RuleType, r.RiskLevel, r.Description,
			boolToInt(r.Immutable))
		if err != nil {
			return err
		}
	}

	// Seed financial patterns
	for _, p := range rules.FinancialPatterns {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO financial_patterns
			(pattern, pattern_type, description)
			VALUES (?, ?, ?)`,
			p.Pattern, p.PatternType, p.Description)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// commandPatternRow maps DB rows to compiledPattern fields.
type commandPatternRow struct {
	Pattern     string `db:"pattern"`
	RiskLevel   int    `db:"risk_level"`
	Category    string `db:"category"`
	Description string `db:"description"`
	Immutable   int    `db:"immutable"`
}

// compilePatterns pre-compiles regex patterns for performance.
func (e *Engine) compilePatterns() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Compile command patterns
	e.compiledCommands = nil
	var patterns []commandPatternRow
	err := e.db.Select(&patterns, `
		SELECT pattern, risk_level, category, description, immutable
		FROM command_patterns
		WHERE enabled = 1
		ORDER BY risk_level DESC`)
	if err != nil {
		return err
	}

	for _, p := range patterns {
		compiled, err := regexp.Compile("(?i)" + p.Pattern)
		if err != nil {
			e.logger.Warn("Invalid command pattern regex", "pattern", p.Pattern, "error", err)
			continue
		}

		e.compiledCommands = append(e.compiledCommands, compiledPattern{
			Pattern:     compiled,
			RiskLevel:   RiskLevel(p.RiskLevel),
			Category:    p.Category,
			Description: p.Description,
			Immutable:   p.Immutable == 1,
		})
	}

	// Compile financial patterns
	var financialPatterns []struct{ Pattern string `db:"pattern"` }
	err = e.db.Select(&financialPatterns, `SELECT pattern FROM financial_patterns WHERE enabled = 1`)
	if err != nil {
		return err
	}

	e.compiledFinancial = nil
	for _, fp := range financialPatterns {
		compiled, err := regexp.Compile("(?i)" + fp.Pattern)
		if err != nil {
			e.logger.Warn("Invalid financial pattern regex", "pattern", fp.Pattern, "error", err)
			continue
		}
		e.compiledFinancial = append(e.compiledFinancial, compiled)
	}

	return nil
}

// Check performs a full permission check pipeline.
func (e *Engine) Check(action, toolName string, details map[string]string, conversationID string) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Stage 1: Immutable financial check
	if decision := e.checkFinancial(details); decision != nil {
		e.logDecision(*decision, action, toolName, details, conversationID)
		return *decision
	}

	// Stage 2: Base rule lookup
	baseRisk, baseConfirm := e.lookupBaseRule(action, toolName)

	// Stage 3: Context analysis
	effectiveRisk := baseRisk
	ruleSource := "base_rule"

	if action == ActionShellExecute {
		cmd := details["command"]
		cmdRisk, cmdSource, cmdImmutable := e.evaluateCommand(cmd)
		if cmdSource != ActionShellExecute {
			// A specific command pattern matched; use its risk level
			// (may raise or lower the base rule risk)
			effectiveRisk = cmdRisk
			ruleSource = "command_pattern:" + cmdSource
		}
		if cmdImmutable {
			decision := Decision{
				Allowed:    false,
				Reason:     "Command matches immutable rule: " + cmdSource,
				RiskLevel:  cmdRisk,
				RuleSource: RuleSourceImmutable,
			}
			e.logDecision(decision, action, toolName, details, conversationID)
			return decision
		}
	}

	// Path-based checks
	if action == ActionFileRead || action == ActionFileWrite || action == ActionFileDelete {
		if path := details["path"]; path != "" {
			if decision := e.checkPath(path, action); decision != nil {
				e.logDecision(*decision, action, toolName, details, conversationID)
				return *decision
			}
		}
	}

	// Fence check: enforce project worktree boundary
	if e.fenceChecker != nil {
		fc := e.fenceChecker
		var fenceErr error

		switch action {
		case ActionFileRead, ActionFileWrite, ActionFileDelete:
			if path := details["path"]; path != "" {
				fenceOp := "read"
				switch action {
				case ActionFileWrite:
					fenceOp = "write"
				case ActionFileDelete:
					fenceOp = "delete"
				}
				fenceErr = fc.CheckPath(path, fenceOp)
			}
		case ActionShellExecute:
			workDir := details["workdir"]
			if workDir == "" {
				workDir = "."
			}
			fenceErr = fc.CheckCommand(details["command"], workDir)
		}

		if fenceErr != nil {
			decision := Decision{
				Allowed:    false,
				Reason:     fenceErr.Error(),
				RiskLevel:  RiskHigh,
				RuleSource: "fence",
			}
			e.logDecision(decision, action, toolName, details, conversationID)
			return decision
		}
	}

	// Stage 4: Override check
	if decision := e.checkOverrides(action, details); decision != nil {
		e.logDecision(*decision, action, toolName, details, conversationID)
		return *decision
	}

	// Stage 5: Confirmation gate
	if e.needsConfirmation(effectiveRisk) || baseConfirm {
		decision := Decision{
			Allowed:              false,
			Reason:               fmt.Sprintf("Action '%s' has risk level %s and requires user confirmation", action, effectiveRisk.String()),
			RiskLevel:            effectiveRisk,
			RuleSource:           "confirmation_gate",
			RequiresConfirmation: true,
		}
		e.logDecision(decision, action, toolName, details, conversationID)
		return decision
	}

	// Permitted
	decision := Decision{
		Allowed:    true,
		Reason:     "Permitted",
		RiskLevel:  effectiveRisk,
		RuleSource: ruleSource,
	}
	e.logDecision(decision, action, toolName, details, conversationID)
	return decision
}

// checkFinancial returns a deny decision if details contain financial operations.
// SEC-1 FIX: Respects BlockFinancial config field - if nil or false, returns nil early.
func (e *Engine) checkFinancial(details map[string]string) *Decision {
	// Honor the BlockFinancial config flag
	if e.config == nil || !e.config.BlockFinancial {
		return nil
	}

	for _, value := range details {
		for _, pattern := range e.compiledFinancial {
			if pattern.MatchString(value) {
				return &Decision{
					Allowed:    false,
					Reason:     "Financial operations are blocked by policy",
					RiskLevel:  RiskCritical,
					RuleSource: RuleSourceImmutable,
				}
			}
		}
	}
	return nil
}

// toolRuleRow maps DB rows for tool_rule lookups.
type toolRuleRow struct {
	RiskLevel           int `db:"risk_level"`
	RequiresConfirmation int `db:"requires_confirmation"`
}

// lookupBaseRule looks up the base risk level for an action.
func (e *Engine) lookupBaseRule(action, toolName string) (RiskLevel, bool) {
	var row toolRuleRow

	// Try exact action match first
	err := e.db.Get(&row, `
		SELECT risk_level, requires_confirmation
		FROM tool_rules
		WHERE action = ? AND enabled = 1
		LIMIT 1`, action)

	if err == nil {
		return RiskLevel(row.RiskLevel), row.RequiresConfirmation == 1
	}

	// Try tool_name match
	if toolName != "" {
		err = e.db.Get(&row, `
			SELECT risk_level, requires_confirmation
			FROM tool_rules
			WHERE tool_name = ? AND enabled = 1
			LIMIT 1`, toolName)

		if err == nil {
			return RiskLevel(row.RiskLevel), row.RequiresConfirmation == 1
		}
	}

	e.logger.Warn("No rule found for action; defaulting to MEDIUM", "action", action, "tool", toolName)
	return RiskMedium, false
}

// evaluateCommand evaluates a shell command against compiled patterns.
func (e *Engine) evaluateCommand(command string) (RiskLevel, string, bool) {
	if command == "" {
		return RiskMedium, ActionShellExecute, false
	}

	for _, cp := range e.compiledCommands {
		if cp.Pattern.MatchString(command) {
			return cp.RiskLevel, cp.Description, cp.Immutable
		}
	}

	return RiskMedium, ActionShellExecute, false
}

// normalizePathForComparison ensures a directory path ends with a path separator
// to prevent prefix matching attacks (e.g., /tmp_backup matching /tmp).
// SEC-4 FIX: Proper directory boundary comparison.
func normalizePathForComparison(p string) string {
	if p == "" {
		return p
	}
	if !strings.HasSuffix(p, string(filepath.Separator)) {
		return p + string(filepath.Separator)
	}
	return p
}

// isPathUnderDir checks if path is under or equal to dir using proper boundary checks.
// SEC-4 FIX: Prevents /tmp_backup/secret from matching /tmp.
func isPathUnderDir(path, dir string) bool {
	// Exact match
	if path == dir {
		return true
	}
	// Check if path is under dir (with proper directory boundary)
	normalizedDir := normalizePathForComparison(dir)
	return strings.HasPrefix(path, normalizedDir)
}

// checkPath checks a filesystem path against path rules.
func (e *Engine) checkPath(pathStr, _ string) *Decision {
	resolved := pathutil.ExpandPath(pathStr)
	if absPath, err := filepath.Abs(resolved); err == nil {
		resolved = absPath
	}

	type pathRuleBlock struct {
		Pattern     string `db:"pattern"`
		Description string `db:"description"`
		Immutable   int    `db:"immutable"`
		RiskLevel   int    `db:"risk_level"`
	}

	// Check block rules first (precedence)
	// SEC-5 FIX: Use separate variable for block rows
	var blockRules []pathRuleBlock
	err := e.db.Select(&blockRules, `
		SELECT pattern, description, immutable, risk_level
		FROM path_rules
		WHERE rule_type = 'block' AND enabled = 1`)
	if err != nil {
		e.logger.Error("Failed to query path rules", "error", err)
		return &Decision{
			Allowed:    false,
			Reason:     ReasonPathRuleQueryFailed,
			RiskLevel:  RiskHigh,
			RuleSource: RuleSourceFailClosed,
		}
	}

	var blocked *Decision
	for _, pr := range blockRules {
		expandedPattern := pathutil.ExpandPath(pr.Pattern)
		if matched, _ := filepath.Match(expandedPattern, resolved); matched {
			ruleSource := "path_rule"
			if pr.Immutable == 1 {
				ruleSource = "immutable"
			}
			blocked = &Decision{
				Allowed:    false,
				Reason:     fmt.Sprintf("Path blocked: %s (pattern: %s)", pr.Description, pr.Pattern),
				RiskLevel:  RiskLevel(pr.RiskLevel),
				RuleSource: ruleSource,
			}
			break
		}

		// SEC-4 FIX: Use proper directory boundary comparison
		if isPathUnderDir(resolved, expandedPattern) {
			ruleSource := "path_rule"
			if pr.Immutable == 1 {
				ruleSource = "immutable"
			}
			blocked = &Decision{
				Allowed:    false,
				Reason:     fmt.Sprintf("Path blocked: %s (pattern: %s)", pr.Description, pr.Pattern),
				RiskLevel:  RiskLevel(pr.RiskLevel),
				RuleSource: ruleSource,
			}
			break
		}
	}
	if blocked != nil {
		return blocked
	}

	// Check allow rules
	// SEC-5 FIX: Use separate variable for allow rows
	hasAllowRules := false
	var allowPatterns []struct{ Pattern string `db:"pattern"` }
	err = e.db.Select(&allowPatterns, `
		SELECT pattern
		FROM path_rules
		WHERE rule_type = 'allow' AND enabled = 1`)
	if err != nil {
		e.logger.Error("Failed to query allow path rules", "error", err)
		return &Decision{
			Allowed:    false,
			Reason:     ReasonPathRuleQueryFailed,
			RiskLevel:  RiskHigh,
			RuleSource: RuleSourceFailClosed,
		}
	}
	hasAllowRules = len(allowPatterns) > 0

	for _, ap := range allowPatterns {
		expandedPattern := pathutil.ExpandPath(ap.Pattern)
		if matched, _ := filepath.Match(expandedPattern, resolved); matched {
			return nil // Allowed
		}
		// SEC-4 FIX: Use proper directory boundary comparison
		if isPathUnderDir(resolved, expandedPattern) {
			return nil // Allowed
		}
	}

	if hasAllowRules {
		return &Decision{
			Allowed:    false,
			Reason:     "Path does not match any allowed pattern",
			RiskLevel:  RiskMedium,
			RuleSource: "path_rule",
		}
	}

	return nil
}

// permissionOverrideRow maps DB rows for permission_overrides.
type permissionOverrideRow struct {
	ID          int64          `db:"id"`
	Pattern     string         `db:"pattern"`
	Decision    string         `db:"decision"`
	Reason      string         `db:"reason"`
	UsageCount  int            `db:"usage_count"`
	MaxUses     int            `db:"max_uses"`
	ExpiresAt   sql.NullString `db:"expires_at"`
}

// checkOverrides checks for creator permission overrides.
// SEC-6 FIX: Uses atomic UPDATE...WHERE to prevent TOCTOU race on usage_count.
// SQLITE-FIX: Two-phase approach — drain the read cursor into memory first, close it,
// then perform writes. Prevents SQLITE_BUSY from open cursor blocking writes.
func (e *Engine) checkOverrides(action string, details map[string]string) *Decision {
	// Phase 1: Collect matching overrides into memory.
	var overrides []permissionOverrideRow
	err := e.db.Select(&overrides, `
		SELECT id, pattern, decision, reason, usage_count, max_uses, expires_at
		FROM permission_overrides
		WHERE action = ? AND (expires_at IS NULL OR datetime(expires_at) > datetime('now'))
		ORDER BY created_at DESC`, action)
	if err != nil {
		return nil
	}

	type overrideCandidate struct {
		id          int64
		decisionStr string
		reason      string
	}
	var candidates []overrideCandidate

	for _, o := range overrides {
		// Check max_uses (preliminary check - atomic check below)
		if o.MaxUses > 0 && o.UsageCount >= o.MaxUses {
			continue
		}

		// Check pattern match
		if o.Pattern != "*" {
			detailsJSON, _ := json.Marshal(details)
			detailStr := string(detailsJSON)

			matched := false

			// Strict mode: use only glob/exact matching (opt-in via config)
			if e.config != nil && e.config.StrictOverrideMatching {
				// Try exact match first
				if detailStr == o.Pattern {
					matched = true
				} else {
					// Try glob matching against the full JSON details
					if m, _ := filepath.Match(o.Pattern, detailStr); m {
						matched = true
					}
				}

				// If no match on full JSON, try matching against individual detail values
				if !matched {
					for _, v := range details {
						if m, _ := filepath.Match(o.Pattern, v); m {
							matched = true
							break
						}
						// Also try exact match on individual values
						if v == o.Pattern {
							matched = true
							break
						}
					}
				}
			} else {
				// Legacy lenient mode: three-strategy cascade
				// SEC-3 FIX: Use exact match or glob match instead of contains()
				// to prevent substring bypass attacks
				if detailStr == o.Pattern {
					matched = true
				} else if m, _ := filepath.Match(o.Pattern, detailStr); m {
					matched = true
				} else {
					// Try matching against individual detail values
					for _, v := range details {
						if v == o.Pattern {
							matched = true
							break
						}
						if m, _ := filepath.Match(o.Pattern, v); m {
							matched = true
							break
						}
					}
				}
			}

			if !matched {
				continue
			}
		}

		candidates = append(candidates, overrideCandidate{
			id:          o.ID,
			decisionStr: o.Decision,
			reason:      o.Reason,
		})
	}

	// Phase 2: Cursor is now closed. Attempt atomic usage count increment
	// for each candidate. The UPDATE is safe because no read cursor is open.
	for _, c := range candidates {
		// SEC-6 FIX: Atomic usage count increment with max_uses check.
		// Only increment if usage_count < max_uses (or max_uses is 0/unlimited).
		// If no rows affected, the override was already exhausted by another concurrent request.
		result, uerr := e.db.Exec(`
			UPDATE permission_overrides
			SET usage_count = usage_count + 1,
			    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ? AND (max_uses = 0 OR usage_count < max_uses)`, c.id)
		if uerr != nil {
			e.logger.Warn("failed to update permission_overrides usage_count",
				"override_id", c.id, "error", uerr)
			continue
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			// Override was already exhausted (max_uses reached) - skip to next
			continue
		}

		if c.decisionStr == DecisionAllow {
			reasonStr := "Creator pre-approved"
			if c.reason != "" {
				reasonStr = "Creator override: " + c.reason
			}
			return &Decision{
				Allowed:         true,
				Reason:          reasonStr,
				RiskLevel:       RiskMedium,
				RuleSource:      "override",
				OverrideApplied: true,
				OverrideID:      &c.id,
			}
		}
		reasonStr := "Creator denied"
		if c.reason != "" {
			reasonStr = "Creator override (deny): " + c.reason
		}
		return &Decision{
			Allowed:         false,
			Reason:          reasonStr,
			RiskLevel:       RiskHigh,
			RuleSource:      "override",
			OverrideApplied: true,
			OverrideID:      &c.id,
		}
	}

	return nil
}

// needsConfirmation determines if the risk level triggers the confirmation gate.
func (e *Engine) needsConfirmation(riskLevel RiskLevel) bool {
	if e.config == nil {
		return riskLevel >= RiskHigh
	}

	if riskLevel >= RiskCritical && e.config.RequireConfirmationCritical {
		return true
	}
	if riskLevel >= RiskHigh && e.config.RequireConfirmationHigh {
		return true
	}
	return false
}

// logDecision writes a permission decision to the audit log.
func (e *Engine) logDecision(decision Decision, action, toolName string, details map[string]string, conversationID string) {
	decisionStr := "allow"
	if !decision.Allowed {
		if decision.RequiresConfirmation {
			decisionStr = "escalate"
		} else {
			decisionStr = "deny"
		}
	}

	detailsJSON, err := json.Marshal(details)
	if err != nil {
		detailsJSON = []byte("{}")
	}

	var overrideID sql.NullInt64
	if decision.OverrideID != nil {
		overrideID.Int64 = *decision.OverrideID
		overrideID.Valid = true
	}

	var convID sql.NullString
	if conversationID != "" {
		convID.String = conversationID
		convID.Valid = true
	}

	_, err = e.db.Exec(`
		INSERT INTO decision_log
		(action, tool_name, details_json, risk_level, decision, reason,
		 rule_source, override_id, conversation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		action, toolName, string(detailsJSON), int(decision.RiskLevel),
		decisionStr, decision.Reason, decision.RuleSource,
		overrideID, convID)

	if err != nil {
		e.logger.Error("Failed to log decision", "error", err)
	}

	if !decision.Allowed {
		e.logger.Info("Permission decision",
			"decision", decisionStr,
			"action", action,
			"tool", toolName,
			"reason", decision.Reason,
			"source", decision.RuleSource)
	} else {
		e.logger.Debug("Permission allow",
			"action", action,
			"tool", toolName,
			"source", decision.RuleSource)
	}
}

// AllowOnce records a temporary allow override for an action.
func (e *Engine) AllowOnce(action, pattern, reason string, maxUses, expiresDays int) (int64, error) {
	return e.RecordOverride(action, pattern, "allow", reason, "", maxUses, expiresDays)
}

// BlockAction records a permanent block for an action.
func (e *Engine) BlockAction(action, pattern, reason string) (int64, error) {
	return e.RecordOverride(action, pattern, "deny", reason, "", 0, 0)
}

// RecordOverride records a creator permission override.
func (e *Engine) RecordOverride(action, pattern, decision, reason, conversationID string, maxUses, expiresDays int) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var expiresAt sql.NullString
	if expiresDays > 0 {
		expiresAt.String = time.Now().UTC().Add(time.Duration(expiresDays) * 24 * time.Hour).Format(time.RFC3339)
		expiresAt.Valid = true
	}

	var convID sql.NullString
	if conversationID != "" {
		convID.String = conversationID
		convID.Valid = true
	}

	result, err := e.db.Exec(`
		INSERT INTO permission_overrides
		(action, pattern, decision, reason, max_uses, expires_at, conversation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		action, pattern, decision, reason, maxUses, expiresAt, convID)

	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	e.logger.Info("Override recorded",
		"decision", decision,
		"action", action,
		"pattern", pattern,
		"reason", reason,
		"id", id)

	return id, nil
}

// GetDecision retrieves a cached decision lookup (for action-only checks).
func (e *Engine) GetDecision(action string) Decision {
	return e.Check(action, "", nil, "")
}

// GetContextForLLM generates a minimal context string for the LLM.
func (e *Engine) GetContextForLLM(decision Decision, action string, details map[string]string) string {
	var lines []string
	lines = append(lines, "# Security Context (current action)", fmt.Sprintf("- Action: %s", action))

	switch action {
	case ActionShellExecute:
		if cmd := details["command"]; cmd != "" {
			if len(cmd) > 100 {
				cmd = cmd[:100] + "..."
			}
			lines = append(lines, fmt.Sprintf("- Command: %s", cmd))
		}
	case "file_read", "file_write", "file_delete":
		if path := details["path"]; path != "" {
			lines = append(lines, fmt.Sprintf("- Path: %s", path))
		}
	}

	lines = append(lines, fmt.Sprintf("- Risk: %s", decision.RiskLevel.String()))

	if decision.Allowed {
		lines = append(lines, "- Status: Permitted")
	} else {
		lines = append(lines, fmt.Sprintf("- Status: Denied - %s", decision.Reason))
	}

	if decision.OverrideApplied {
		lines = append(lines, "- Note: Creator override is active for this action")
	}

	if !decision.Allowed {
		lines = append(lines, "- Do not attempt to work around this restriction.")
	}

	return strings.Join(lines, "\n")
}

// Close closes the database connection.
func (e *Engine) Close() error {
	return e.db.Close()
}

// boolToInt converts a boolean to an integer (0 or 1).
// Compile-time assertion that Engine implements io.Closer.
var _ io.Closer = (*Engine)(nil)

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Schema SQL for the security database.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS tool_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name   TEXT NOT NULL,
    action      TEXT NOT NULL,
    risk_level  INTEGER NOT NULL DEFAULT 2,
    description TEXT NOT NULL DEFAULT '',
    requires_confirmation INTEGER NOT NULL DEFAULT 0,
    immutable   INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(tool_name, action)
);

CREATE TABLE IF NOT EXISTS command_patterns (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern      TEXT NOT NULL,
    pattern_type TEXT NOT NULL DEFAULT 'regex',
    risk_level   INTEGER NOT NULL,
    category     TEXT NOT NULL DEFAULT 'general',
    description  TEXT NOT NULL DEFAULT '',
    immutable    INTEGER NOT NULL DEFAULT 0,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(pattern, pattern_type)
);

CREATE TABLE IF NOT EXISTS path_rules (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern     TEXT NOT NULL,
    rule_type   TEXT NOT NULL,
    risk_level  INTEGER NOT NULL DEFAULT 2,
    description TEXT NOT NULL DEFAULT '',
    immutable   INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(pattern, rule_type)
);

CREATE TABLE IF NOT EXISTS permission_overrides (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    action          TEXT NOT NULL,
    pattern         TEXT NOT NULL DEFAULT '*',
    decision        TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    usage_count     INTEGER NOT NULL DEFAULT 0,
    max_uses        INTEGER NOT NULL DEFAULT 50,
    expires_at      TEXT,
    conversation_id TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS decision_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    action          TEXT NOT NULL,
    tool_name       TEXT NOT NULL DEFAULT '',
    details_json    TEXT NOT NULL DEFAULT '{}',
    risk_level      INTEGER NOT NULL,
    decision        TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    rule_source     TEXT NOT NULL DEFAULT '',
    override_id     INTEGER,
    conversation_id TEXT
);

CREATE TABLE IF NOT EXISTS financial_patterns (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern      TEXT NOT NULL,
    pattern_type TEXT NOT NULL DEFAULT 'regex',
    description  TEXT NOT NULL DEFAULT '',
    immutable    INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    UNIQUE(pattern, pattern_type)
);

CREATE INDEX IF NOT EXISTS idx_decision_log_action ON decision_log(action);
CREATE INDEX IF NOT EXISTS idx_decision_log_timestamp ON decision_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_overrides_action ON permission_overrides(action);
`
