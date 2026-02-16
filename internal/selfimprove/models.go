// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"time"
)

// IssueType represents the type of detected issue.
type IssueType string

const (
	IssueTypeError      IssueType = "error"
	IssueTypePerformance IssueType = "performance"
	IssueTypeReliability IssueType = "reliability"
	IssueTypeSecurity   IssueType = "security"
	IssueTypeUsability  IssueType = "usability"
)

// IssueSeverity represents the severity of an issue.
type IssueSeverity string

const (
	SeverityLow      IssueSeverity = "low"
	SeverityMedium   IssueSeverity = "medium"
	SeverityHigh     IssueSeverity = "high"
	SeverityCritical IssueSeverity = "critical"
)

// Issue represents a detected issue.
type Issue struct {
	ID          string        `json:"id"`
	Type        IssueType     `json:"type"`
	Severity    IssueSeverity `json:"severity"`
	Description string        `json:"description"`
	Source      string        `json:"source"`      // File, log, metric source
	Context     string        `json:"context"`     // Surrounding code/log lines
	DetectedAt  time.Time     `json:"detected_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// RootCauseAnalysis represents the analysis of an issue's root cause.
type RootCauseAnalysis struct {
	IssueID       string   `json:"issue_id"`
	RootCause     string   `json:"root_cause"`
	Contributing  []string `json:"contributing_factors"`
	AffectedFiles []string `json:"affected_files"`
	Confidence    float64  `json:"confidence"` // 0.0 - 1.0
	AnalyzedAt    time.Time `json:"analyzed_at"`
}

// FixType represents the type of fix.
type FixType string

const (
	FixTypeCodeChange  FixType = "code_change"
	FixTypeConfigChange FixType = "config_change"
	FixTypeRefactor    FixType = "refactor"
	FixTypeWorkaround  FixType = "workaround"
)

// ProposedFix represents a proposed fix for an issue.
type ProposedFix struct {
	ID          string   `json:"id"`
	IssueID     string   `json:"issue_id"`
	AnalysisID  string   `json:"analysis_id"`
	Type        FixType  `json:"type"`
	Description string   `json:"description"`
	Diff        string   `json:"diff"`       // Unified diff format
	FilePath    string   `json:"file_path"`  // Primary file being modified
	Risk        string   `json:"risk"`       // low, medium, high
	GeneratedAt time.Time `json:"generated_at"`
}

// ValidationStatus represents the status of fix validation.
type ValidationStatus string

const (
	ValidationPending  ValidationStatus = "pending"
	ValidationPassed   ValidationStatus = "passed"
	ValidationFailed   ValidationStatus = "failed"
	ValidationSkipped  ValidationStatus = "skipped"
)

// ValidationResult represents the result of validating a fix.
type ValidationResult struct {
	FixID         string            `json:"fix_id"`
	Success       bool              `json:"success"`
	Status        ValidationStatus  `json:"status"`
	TestsPassed   int              `json:"tests_passed"`
	TestsFailed   int              `json:"tests_failed"`
	BuildSuccess  bool             `json:"build_success"`
	Errors        []string         `json:"errors,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	ValidatedAt   time.Time        `json:"validated_at"`
	Duration      time.Duration    `json:"duration"`
}

// AppliedFix represents a fix that has been applied.
type AppliedFix struct {
	FixID       string    `json:"fix_id"`
	AppliedAt   time.Time `json:"applied_at"`
	ApprovedBy  string    `json:"approved_by"` // "auto" or user ID
	CommitHash  string    `json:"commit_hash,omitempty"`
	RollbackAvailable bool `json:"rollback_available"`
	BackupPath  string    `json:"backup_path,omitempty"`
}

// CycleStatus represents the status of an improvement cycle.
type CycleStatus string

const (
	CycleStatusRunning   CycleStatus = "running"
	CycleStatusCompleted CycleStatus = "completed"
	CycleStatusFailed    CycleStatus = "failed"
	CycleStatusCancelled CycleStatus = "cancelled"
)

// ImprovementCycle represents a complete improvement cycle.
type ImprovementCycle struct {
	ID              string      `json:"id"`
	Status          CycleStatus `json:"status"`
	StartedAt       time.Time   `json:"started_at"`
	CompletedAt     *time.Time  `json:"completed_at,omitempty"`
	IssuesDetected  int         `json:"issues_detected"`
	IssuesAnalyzed  int         `json:"issues_analyzed"`
	FixesGenerated  int         `json:"fixes_generated"`
	FixesValidated  int         `json:"fixes_validated"`
	FixesApplied    int         `json:"fixes_applied"`
	Error           string      `json:"error,omitempty"`
}

// ControllerStatus represents the current status of the controller.
type ControllerStatus struct {
	CurrentCycle        *ImprovementCycle `json:"current_cycle,omitempty"`
	IssuesCount         int              `json:"issues_count"`
	AnalysesCount       int              `json:"analyses_count"`
	FixesCount          int              `json:"fixes_count"`
	ValidationsCount    int              `json:"validations_count"`
	AppliedCount        int              `json:"applied_count"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	CircuitBreakerTripped bool          `json:"circuit_breaker_tripped"`
	FailedIssues        map[string]int   `json:"failed_issues"`
	PendingApprovals    []string         `json:"pending_approvals"`
	CyclesCompleted     int              `json:"cycles_completed"`
}

// ToMap converts the status to a map for JSON serialization.
func (s *ControllerStatus) ToMap() map[string]any {
	return map[string]any{
		"current_cycle":          s.CurrentCycle,
		"issues_count":           s.IssuesCount,
		"analyses_count":         s.AnalysesCount,
		"fixes_count":            s.FixesCount,
		"validations_count":      s.ValidationsCount,
		"applied_count":          s.AppliedCount,
		"consecutive_failures":   s.ConsecutiveFailures,
		"circuit_breaker_tripped": s.CircuitBreakerTripped,
		"failed_issues":          s.FailedIssues,
		"pending_approvals":      s.PendingApprovals,
		"cycles_completed":       s.CyclesCompleted,
	}
}
