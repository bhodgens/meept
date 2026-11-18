package kanban

import (
	"slices"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/config"
)

// C5 Grant constants.
const (
	GrantBoardRead     = "board.read"
	GrantBoardMutate   = "board.mutate"
	GrantBoardTakeover = "board.takeover"
)

// Authorizer enforces C5 grants for kanban boards.
type Authorizer struct {
	cfg   *config.Config
	users *auth.Store
}

// NewAuthorizer creates a new Authorizer.
// If users is nil, the authorizer operates in owner-only mode (multiuser.enabled=false).
func NewAuthorizer(cfg *config.Config, users *auth.Store) *Authorizer {
	return &Authorizer{
		cfg:   cfg,
		users: users,
	}
}

// CheckGrant returns true if the principal has the specified C5 grant.
// In multiuser off mode, any principal is granted the C5 grants.
func (a *Authorizer) CheckGrant(principal string, grant string) bool {
	if !a.isC5Grant(grant) {
		return false
	}

	// Multiuser OFF mode (default) or owner-only mode (nil users)
	// returns true for all C5 grants for any principal.
	if a.cfg == nil || !a.cfg.MultiUser.Enabled || a.users == nil {
		return true
	}

	// Multiuser ON mode: check the user's assigned groups/grants.
	// We treat the principal string as a UserID.
	idn := &auth.Identity{UserID: principal}
	return slices.Contains(a.users.UserGroups(idn), grant)
}

// GetGrantsForPrincipal returns the C5 grants applicable to the principal.
// In multiuser off mode, it returns all three C5 grants.
func (a *Authorizer) GetGrantsForPrincipal(principal string) []string {
	// If Multiuser is OFF or we are in owner-only mode, return all C5 grants.
	if a.cfg == nil || !a.cfg.MultiUser.Enabled || a.users == nil {
		return []string{GrantBoardRead, GrantBoardMutate, GrantBoardTakeover}
	}

	if principal == "" {
		return []string{}
	}

	idn := &auth.Identity{UserID: principal}
	groups := a.users.UserGroups(idn)

	// Filter to C5 grants: the store's Groups field carries agent-routing
	// group names too (see internal/agent group gates); those are not board
	// grants and must not leak through this surface. UserGroups already
	// returns a fresh copy; we build a second filtered slice.
	res := make([]string, 0, len(groups))
	for _, g := range groups {
		if a.isC5Grant(g) {
			res = append(res, g)
		}
	}
	return res
}

func (a *Authorizer) isC5Grant(grant string) bool {
	return grant == GrantBoardRead || grant == GrantBoardMutate || grant == GrantBoardTakeover
}
