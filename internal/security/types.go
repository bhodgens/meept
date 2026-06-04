// Package security provides the security engine with SQLite-backed decision making,
// audit logging, input sanitization, and prompt injection defense.
package security

import (
	"time"
)

// RiskLevel represents the severity of an action.
//go:generate go run golang.org/x/tools/cmd/stringer -type=RiskLevel
type RiskLevel int

const (
	RiskSafe RiskLevel = iota // SAFE
	RiskLow                   // LOW
	RiskMedium                // MEDIUM
	RiskHigh                  // HIGH
	RiskCritical              // CRITICAL
)

// Decision represents the result of a permission check.
type Decision struct {
	Allowed              bool      `json:"allowed"`
	Reason               string    `json:"reason"`
	RiskLevel            RiskLevel `json:"risk_level"`
	RuleSource           string    `json:"rule_source"` // base_rule, command_pattern, path_rule, override, immutable, confirmation_gate
	RequiresConfirmation bool      `json:"requires_confirmation"`
	OverrideApplied      bool      `json:"override_applied"`
	OverrideID           *int64    `json:"override_id,omitempty"`
}

// AuditEntry represents a single entry in the audit log.
type AuditEntry struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Action         string    `json:"action"`
	ToolName       string    `json:"tool_name"`
	DetailsJSON    string    `json:"details_json"`
	RiskLevel      RiskLevel `json:"risk_level"`
	Decision       string    `json:"decision"` // allow, deny, escalate
	Reason         string    `json:"reason"`
	RuleSource     string    `json:"rule_source"`
	OverrideID     *int64    `json:"override_id,omitempty"`
	ConversationID *string   `json:"conversation_id,omitempty"`
}

// SecurityStats holds aggregate security statistics.
//
//nolint:revive // stutter with package name is intentional for API clarity
type SecurityStats struct {
	TotalDecisions   int64            `json:"total_decisions"`
	TotalAllows      int64            `json:"total_allows"`
	TotalDenies      int64            `json:"total_denies"`
	TotalEscalations int64            `json:"total_escalations"`
	ActiveOverrides  int64            `json:"active_overrides"`
	TopDeniedActions map[string]int64 `json:"top_denied_actions"`
}

// Override represents a creator permission override.
type Override struct {
	ID             int64      `json:"id"`
	Action         string     `json:"action"`
	Pattern        string     `json:"pattern"`
	Decision       string     `json:"decision"` // allow, deny
	Reason         string     `json:"reason"`
	UsageCount     int        `json:"usage_count"`
	MaxUses        int        `json:"max_uses"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	ConversationID *string    `json:"conversation_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ToolRule defines permissions for a tool/action combination.
type ToolRule struct {
	ID                   int64     `json:"id"`
	ToolName             string    `json:"tool_name"`
	Action               string    `json:"action"`
	RiskLevel            RiskLevel `json:"risk_level"`
	Description          string    `json:"description"`
	RequiresConfirmation bool      `json:"requires_confirmation"`
	Immutable            bool      `json:"immutable"`
	Enabled              bool      `json:"enabled"`
}

// CommandPattern defines a regex pattern for matching shell commands.
type CommandPattern struct {
	ID          int64     `json:"id"`
	Pattern     string    `json:"pattern"`
	PatternType string    `json:"pattern_type"` // regex
	RiskLevel   RiskLevel `json:"risk_level"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Immutable   bool      `json:"immutable"`
	Enabled     bool      `json:"enabled"`
}

// PathRule defines a rule for filesystem path access.
type PathRule struct {
	ID          int64     `json:"id"`
	Pattern     string    `json:"pattern"`
	RuleType    string    `json:"rule_type"` // block, allow
	RiskLevel   RiskLevel `json:"risk_level"`
	Description string    `json:"description"`
	Immutable   bool      `json:"immutable"`
	Enabled     bool      `json:"enabled"`
}

// FinancialPattern defines a pattern for detecting financial operations.
type FinancialPattern struct {
	ID          int64  `json:"id"`
	Pattern     string `json:"pattern"`
	PatternType string `json:"pattern_type"` // regex
	Description string `json:"description"`
	Immutable   bool   `json:"immutable"`
	Enabled     bool   `json:"enabled"`
}

// QueryFilters holds filters for querying the audit log.
type QueryFilters struct {
	Action   string
	Decision string
	Since    *time.Time
	Limit    int
}
