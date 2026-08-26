package auth

import (
	"context"
	"testing"
)

var _ PermissionChecker = OwnerOnlyPermissions{} // interface conformance

func TestOwnerOnlyOwnerAccessAllowed(t *testing.T) {
	p := OwnerOnlyPermissions{}
	actor := &Identity{UserID: "user-1", UserName: "alice"}

	if !p.CanAccess(context.Background(), actor, "user-1") {
		t.Fatal("owner denied access to own resource")
	}
}

func TestOwnerOnlyNonOwnerDenied(t *testing.T) {
	p := OwnerOnlyPermissions{}
	actor := &Identity{UserID: "user-2"}

	if p.CanAccess(context.Background(), actor, "user-1") {
		t.Fatal("non-owner granted access to another user's resource")
	}
}

func TestOwnerOnlyNilActorSeesEverything(t *testing.T) {
	p := OwnerOnlyPermissions{}

	if !p.CanAccess(context.Background(), nil, "user-1") {
		t.Fatal("nil actor denied in legacy single-user mode")
	}
	if !p.CanAccess(context.Background(), nil, "") {
		t.Fatal("nil actor denied unowned resource in legacy mode")
	}
}

func TestOwnerOnlyEmptyResourceDeniedForActor(t *testing.T) {
	p := OwnerOnlyPermissions{}
	actor := &Identity{UserID: "user-1"}

	// An unowned resource is not "owned by user-1"; multi-user actors must
	// not see it even when their own id is the empty string comparison
	// target.
	if p.CanAccess(context.Background(), actor, "") {
		t.Fatal("actor granted access to unowned resource")
	}
}
