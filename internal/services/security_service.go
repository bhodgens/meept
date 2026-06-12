package services

import (
	"context"

	"github.com/caimlas/meept/pkg/security"
)

// SecurityService handles security operations.
type SecurityService struct {
	checker *security.PermissionChecker
}

// NewSecurityService creates a security service.
func NewSecurityService(c *security.PermissionChecker) *SecurityService {
	return &SecurityService{checker: c}
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
			Allowed: true,
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
	// TODO: Implement actual audit log retrieval
	return []AuditEntry{}, nil
}
