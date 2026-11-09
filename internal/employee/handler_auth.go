// Package employee provides the AI employee framework: constitution-bound
// persistent agents with goal loops, enforcement, and audit.
//
// This file implements caller authorization for RPC handlers.
// All state-changing operations validate that the caller is authorized
// to perform the operation on the specified employee.
package employee

import (
	"context"
	"errors"
	"slices"
	"time"

	stdhttp "net/http"

	"github.com/caimlas/meept/internal/auth"
	commhttp "github.com/caimlas/meept/internal/comm/http"
)

// callerKey is the context key for storing the caller identity.
type callerKey struct{}

// errCallerUnresolved is returned when a presented API key does not resolve
// to a store identity. Callers must fail closed and must not substitute a
// synthetic id. The error text never includes key material.
var errCallerUnresolved = errors.New("unauthenticated: api key does not resolve to a store identity")

// CallerInfo holds information about the caller making an RPC request.
type CallerInfo struct {
	// APIKey is the presented credential, when the request carried one.
	// Authentication material: never log this field.
	APIKey string
	// UserID is the auth-store identity for this caller. It is never
	// invented from key material. Empty when the request has no resolved
	// identity.
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

// presentedAPIKey returns the raw key the HTTP boundary already accepted,
// or the key on the request when the boundary stashed only an identity.
// The value is authentication material: do not log it.
func presentedAPIKey(ctx context.Context, r *stdhttp.Request) string {
	if key, ok := commhttp.APIKeyFromContext(ctx); ok && key != "" {
		return key
	}
	if r == nil {
		return ""
	}
	return commhttp.ExtractKeyFromRequest(r)
}

// resolveCaller returns the store identity for an employee HTTP request.
//
// A store-validated *auth.Identity already on the context (multi-user
// StoreAuth, which called Store.Validate) wins: its UserID is used as-is.
// Otherwise a presented raw key is resolved through store.Validate. A key
// the store does not match fails closed (errCallerUnresolved). The synthetic
// id "user" is never invented from key material.
//
// No key and no identity returns (nil, nil); RPC handlers decide.
// A nil store cannot resolve a key (legacy flat-key mode, multi-user off).
// A presented key is left unresolved rather than rejected here so the
// flat-key HTTP path stays dispatchable; privileged RPC checks fail closed
// on a nil caller.
func resolveCaller(ctx context.Context, rawKey string, store *auth.Store) (*CallerInfo, error) {
	if idn, ok := commhttp.IdentityFromContext(ctx); ok && idn.UserID != "" {
		return &CallerInfo{APIKey: rawKey, UserID: idn.UserID}, nil
	}
	if rawKey == "" {
		return nil, nil
	}
	if store == nil {
		return nil, nil
	}
	idn, err := store.Validate(rawKey, time.Now())
	if err != nil || idn == nil || idn.UserID == "" {
		return nil, errCallerUnresolved
	}
	return &CallerInfo{APIKey: rawKey, UserID: idn.UserID}, nil
}

// isAuthorizedApprover checks if the caller is authorized to approve
// plans for the given employee. Authorization rules:
// 1. "user" (primary API key holder) can always approve
// 2. Anyone in the employee's escalates_to list can approve
//
//lint:ignore U1000 Used by handler.go for plan approval validation
func isAuthorizedApprover(callerID string, escalatesTo []string) bool {
	if callerID == "user" {
		return true
	}
	return slices.Contains(escalatesTo, callerID)
}

