package services

import (
	"context"
	"database/sql"

	"github.com/caimlas/meept/pkg/security"
)

// SecurityService handles security operations.
type SecurityService struct {
	checker *security.PermissionChecker
	auditDB *sql.DB
}

// NewSecurityService creates a security service.
func NewSecurityService(c *security.PermissionChecker) *SecurityService {
	return &SecurityService{checker: c}
}

// SetAuditDB sets the audit database for querying audit entries.
func (s *SecurityService) SetAuditDB(db *sql.DB) {
	if db != nil {
		s.auditDB = db
	}
}

// CheckRequest contains security check parameters.
type CheckRequest struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

// CheckResponse contains security check results.
type CheckResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Check performs a security check.
func (s *SecurityService) Check(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	if req.Action == "" {
		return nil, wrapError("security", "Check", ErrInvalidInput)
	}

	if s.checker == nil {
		return &CheckResponse{
			Allowed: false,
			Reason:  "security checker not available",
		}, nil
	}

	details := map[string]string{
		"resource": req.Resource,
	}
	result := s.checker.CheckPermission(req.Action, details)
	return &CheckResponse{
		Allowed: result.Allowed,
		Reason:  result.Reason,
	}, nil
}

// AuditEntry represents an audit log entry.
type AuditEntry struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Resource  string `json:"resource"`
	Allowed   bool   `json:"allowed"`
}

// AuditRequest contains audit parameters.
type AuditRequest struct {
	Limit int `json:"limit,omitempty"`
}

// Audit returns recent audit entries.
func (s *SecurityService) Audit(ctx context.Context, req AuditRequest) ([]AuditEntry, error) {
	if s.auditDB == nil {
		return []AuditEntry{}, nil
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.auditDB.QueryContext(ctx,
		`SELECT timestamp, event_type, severity, details, source FROM audit_log ORDER BY timestamp DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return []AuditEntry{}, err
	}
	defer rows.Close()

	var result []AuditEntry
	for rows.Next() {
		var timestamp, eventType, severity, detailsJSON, source string
		if err := rows.Scan(&timestamp, &eventType, &severity, &detailsJSON, &source); err != nil {
			continue
		}
		result = append(result, AuditEntry{
			Timestamp: timestamp,
			Action:    eventType,
			Resource:  source,
			Allowed:   severity != "critical" && severity != "error",
		})
	}
	if result == nil {
		result = []AuditEntry{}
	}
	return result, nil
}
