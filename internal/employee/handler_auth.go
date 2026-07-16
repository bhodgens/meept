// Package employee provides the AI employee framework: constitution-bound
// persistent agents with goal loops, enforcement, and audit.
//
// This file implements caller authorization for RPC handlers.
// All state-changing operations validate that the caller is authorized
// to perform the operation on the specified employee.
package employee

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/comm/http"
)

// callerKey is the context key for storing the caller identity.
type callerKey struct{}

// CallerInfo holds information about the caller making an RPC request.
type CallerInfo struct {
	// APIKey is the API key used for authentication.
	APIKey string
	// UserID is the logical user identity derived from the API key.
	// For now, this is the API key itself or "user" for the primary key.
	UserID string
}

// ContextWithCaller returns a new context with the caller information attached.
func ContextWithCaller(ctx context.Context, info *CallerInfo) context.Context {
	return context.WithValue(ctx, callerKey{}, info)
}

// CallerFromContext extracts caller information from context.
// Returns nil if no caller info is present.
func CallerFromContext(ctx context.Context) *CallerInfo {
	if info, ok := ctx.Value(callerKey{}).(*CallerInfo); ok {
		return info
	}
	return nil
}

// extractCallerFromAPIKey extracts caller info from an API key.
// The primary API key is treated as "user", other keys derive user ID from the key.
//
//nolint:unused // Used by api_handlers.go for caller injection
func extractCallerFromAPIKey(apiKey string) *CallerInfo {
	return &CallerInfo{
		APIKey: apiKey,
		UserID: deriveUserIDFromAPIKey(apiKey),
	}
}

// deriveUserIDFromAPIKey derives a logical user identity from an API key.
// This can be extended to support multi-tenant deployments.
//
//nolint:unused // Used by api_handlers.go for caller injection
func deriveUserIDFromAPIKey(apiKey string) string {
	if apiKey == "" {
		return "anonymous"
	}
	// For now, all non-empty keys map to "user"
	// Future: parse key prefix or look up in identity store
	return "user"
}

// injectCallerFromHTTPContext extracts the API key from HTTP context and
// returns a CallerInfo suitable for passing to employee RPC handlers.
// Returns nil if no API key is present in context.
//
//nolint:unused // Used by api_handlers.go for caller injection
func injectCallerFromHTTPContext(ctx context.Context) *CallerInfo {
	apiKey, ok := http.APIKeyFromContext(ctx)
	if !ok || apiKey == "" {
		return nil
	}
	return extractCallerFromAPIKey(apiKey)
}

// isAuthorizedApprover checks if the caller is authorized to approve
// plans for the given employee. Authorization rules:
// 1. "user" (primary API key holder) can always approve
// 2. Anyone in the employee's escalates_to list can approve
//
//nolint:unused // Used by handler.go for plan approval validation
func isAuthorizedApprover(callerID string, escalatesTo []string) bool {
	if callerID == "user" {
		return true
	}
	for _, id := range escalatesTo {
		if id == callerID {
			return true
		}
	}
	return false
}

// isAuthorizedAmmender checks if the caller is authorized to propose
// constitution amendments for the given employee.
//
//nolint:unused // Used by handler.go for constitution amendment validation
func isAuthorizedAmmender(callerID string, employeeID string, escalatesTo []string, selfProposeAllowed bool) bool {
	// User can always amend
	if callerID == "user" {
		return true
	}
	// Self-propose allowed and caller is the employee
	if selfProposeAllowed && callerID == employeeID {
		return true
	}
	// Caller is in escalates_to
	for _, id := range escalatesTo {
		if id == callerID {
			return true
		}
	}
	return false
}

// requireCaller extracts and validates the caller from context.
// Returns error if no caller is present or if caller is not authorized
// for the specified employee operation.
//
//nolint:unused // Used by handler.go for operation-level authorization
func requireCaller(ctx context.Context, emp *Employee, operation string) (*CallerInfo, error) {
	caller := CallerFromContext(ctx)
	if caller == nil {
		return nil, fmt.Errorf("unauthenticated: API key required for %s", operation)
	}

	// Check authorization based on employee's escalates_to
	if !isAuthorizedForOperation(caller.UserID, emp.ID, emp.Constitution.EscalatesTo, operation) {
		return nil, fmt.Errorf("unauthorized: caller %q cannot perform %s on employee %q", caller.UserID, operation, emp.ID)
	}

	return caller, nil
}

// isAuthorizedForOperation checks if a caller can perform an operation on an employee.
//
//nolint:unused // Used by handler.go for operation-level authorization
func isAuthorizedForOperation(callerID, employeeID string, escalatesTo []string, operation string) bool {
	// "user" can perform all operations
	if callerID == "user" {
		return true
	}

	// Some operations (read-only) may be allowed for escalates_to entries
	switch operation {
	case "read", "list":
		// Allow read access to escalates_to entries
		for _, id := range escalatesTo {
			if id == callerID {
				return true
			}
		}
		return false

	case "approve", "reject":
		// Plan approval requires being in escalates_to
		for _, id := range escalatesTo {
			if id == callerID {
				return true
			}
		}
		return false

	case "amend":
		// Amendments handled separately via isAuthorizedAmmender
		return false

	default:
		// All other operations (pause, resume, trigger, delete) require "user"
		return false
	}
}
