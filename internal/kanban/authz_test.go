package kanban

import (
	"testing"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/config"
)

func TestAuthorizer(t *testing.T) {
	// Setup a temporary user store
	tmpDir := t.TempDir()
	store, err := auth.NewStore(tmpDir + "/users.db")
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}

	// Pre-populate a user named bob. NOTE: auth.AddUser generates the ID
	// (pkg/id, "user-" prefix) — the principal the authorizer takes is the
	// store's UserID, never the human Name, mirroring auth.Identity.UserID.
	user, err := store.AddUser("bob")
	if err != nil {
		t.Fatalf("failed to add user bob: %v", err)
	}
	bobID := user.ID

	// bob holds board.read only, plus a non-C5 routing group that must be
	// filtered out of GetGrantsForPrincipal (agent group policies share the
	// Groups field — see internal/auth User.Groups doc).
	err = store.SetUserGroups(user.ID, []string{GrantBoardRead, "admins"})
	if err != nil {
		t.Fatalf("failed to set bob's groups: %v", err)
	}

	tests := []struct {
		name       string
		multiUser  bool
		nilUsers   bool
		principal  string
		grant      string
		wantCheck  bool
		wantGrants []string
	}{
		{
			name:       "Multiuser OFF: owner gets all grants",
			multiUser:  false,
			nilUsers:   false,
			principal:  "any-principal",
			grant:      GrantBoardRead,
			wantCheck:  true,
			wantGrants: []string{GrantBoardRead, GrantBoardMutate, GrantBoardTakeover},
		},
		{
			name:       "Multiuser OFF: nil users gets all grants",
			multiUser:  false,
			nilUsers:   true,
			principal:  "any-principal",
			grant:      GrantBoardTakeover,
			wantCheck:  true,
			wantGrants: []string{GrantBoardRead, GrantBoardMutate, GrantBoardTakeover},
		},
		{
			name:       "Multiuser OFF: nil config gets all grants",
			multiUser:  false,
			nilUsers:   false,
			principal:  "any-principal",
			grant:      GrantBoardRead,
			wantCheck:  true,
			wantGrants: []string{GrantBoardRead, GrantBoardMutate, GrantBoardTakeover},
		},
		{
			name:       "Multiuser ON: known user gets assigned grants",
			multiUser:  true,
			nilUsers:   false,
			principal:  bobID,
			grant:      GrantBoardRead,
			wantCheck:  true,
			wantGrants: []string{GrantBoardRead},
		},
		{
			name:       "Multiuser ON: known user lacks grant",
			multiUser:  true,
			nilUsers:   false,
			principal:  bobID,
			grant:      GrantBoardMutate,
			wantCheck:  false,
			wantGrants: []string{GrantBoardRead},
		},
		{
			name:       "Multiuser ON: unknown user gets nothing",
			multiUser:  true,
			nilUsers:   false,
			principal:  "unknown",
			grant:      GrantBoardRead,
			wantCheck:  false,
			wantGrants: []string{},
		},
		{
			name:       "Multiuser ON: empty principal gets nothing",
			multiUser:  true,
			nilUsers:   false,
			principal:  "",
			grant:      GrantBoardRead,
			wantCheck:  false,
			wantGrants: []string{},
		},
		{
			name:       "Invalid grant check",
			multiUser:  true,
			nilUsers:   false,
			principal:  bobID,
			grant:      "invalid.grant",
			wantCheck:  false,
			wantGrants: []string{GrantBoardRead},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *config.Config
			if !tt.nilUsers {
				cfg = &config.Config{
					MultiUser: config.MultiUserConfig{
						Enabled: tt.multiUser,
					},
				}
			}

			var users *auth.Store
			if !tt.nilUsers {
				users = store
			}

			authz := NewAuthorizer(cfg, users)

			// Test CheckGrant
			if got := authz.CheckGrant(tt.principal, tt.grant); got != tt.wantCheck {
				t.Errorf("CheckGrant(%q, %q) = %v; want %v", tt.principal, tt.grant, got, tt.wantCheck)
			}

			// Test GetGrantsForPrincipal
			gotGrants := authz.GetGrantsForPrincipal(tt.principal)
			if len(gotGrants) != len(tt.wantGrants) {
				t.Errorf("GetGrantsForPrincipal(%q) len = %d; want %d", tt.principal, len(gotGrants), len(tt.wantGrants))
				return
			}
			for i := range gotGrants {
				if gotGrants[i] != tt.wantGrants[i] {
					t.Errorf("GetGrantsForPrincipal(%q) at index %d = %q; want %q", tt.principal, i, gotGrants[i], tt.wantGrants[i])
				}
			}
		})
	}
}

func TestAuthorizer_NoPanic(t *testing.T) {
	// Test nil-pointer safety for various combinations
	authzNilCfg := NewAuthorizer(nil, nil)
	if !authzNilCfg.CheckGrant("any", GrantBoardRead) {
		t.Error("nil config should not panic and should return true in owner-only mode")
	}

	authzNilUsers := NewAuthorizer(&config.Config{MultiUser: config.MultiUserConfig{Enabled: true}}, nil)
	if !authzNilUsers.CheckGrant("any", GrantBoardRead) {
		t.Error("nil users should return true (owner-only mode) even when multiuser is enabled")
	}
}
