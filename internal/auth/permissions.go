package auth

import "context"

// DESIGN ONLY. Future shape candidates: role strings vs capability bits vs
// policy expressions. Only "can access own resources" is enforced today.
type PermissionChecker interface {
	CanAccess(ctx context.Context, actor *Identity, resourceOwner string) bool
}

// OwnerOnlyPermissions enforces the single permission level shipped today:
// an actor may access a resource only when it owns it. A nil actor means
// legacy single-user mode, which sees everything. An unowned resource
// (empty owner) follows the same rule — visible only in single-user mode —
// so enabling multi-user cannot silently expose anonymous resources.
type OwnerOnlyPermissions struct{}

// CanAccess implements PermissionChecker with owner-only semantics.
func (OwnerOnlyPermissions) CanAccess(_ context.Context, actor *Identity, resourceOwner string) bool {
	if actor == nil {
		return true // legacy single-user mode sees everything
	}
	return actor.UserID == resourceOwner && resourceOwner != ""
}
