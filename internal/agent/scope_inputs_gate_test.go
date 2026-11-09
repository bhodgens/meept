package agent

// Gate test for B1 scope-input resolution, retargeted to the WIRED
// constructor (resolveScopeInputsWithCeiling, called by scope_wiring.go for
// every recall surface). The plain resolveScopeInputs wrapper this file
// originally pinned was deleted as deadcode wave-1 (zero callers at HEAD;
// scope_wiring.go consumes the WithCeiling variant exclusively).
//
// Contract (docs/plans/20261030-memory-second-brain/01-scoping, B1 + Flag 3):
//
//	resolveScopeInputs(session) -> (maxScope Scope, projectID, userID string, multiUser bool)
//	Precedence: agent MemoryMaxScope (empty=project); projectID = session's
//	bound project ("" if unbound); userID = multiuser identity ("" in
//	single-user). Unbound session + maxScope=project => filter matches
//	NOTHING project-scoped (global + own-user still recallable). Never fall
//	back to the daemon's CWD. The multiUser bit rides ALONGSIDE userID so
//	the WithScopeFilter predicate can distinguish multiuser-disabled (empty
//	userID matches) from multiuser-enabled-empty-userID (fail closed).
//
// With a nil group ceiling the WithCeiling constructor is behaviorally the
// B1 baseline, so the pin transfers unchanged.

import (
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

func TestResolveScopeInputs(t *testing.T) {
	spec := func(maxScope string) *AgentSpec {
		return &AgentSpec{
			ID:             "scope-pin-agent",
			Name:           "scope pin agent",
			Role:           RoleExecutor,
			Enabled:        true,
			MemoryMaxScope: maxScope,
		}
	}

	tests := []struct {
		name            string
		spec            *AgentSpec
		projectID       string // session's bound project ("" = unbound session)
		userID          string // resolved multiuser identity ("" = single-user)
		wantMax         memory.Scope
		wantProjectID   string
		wantUserID      string
		wantMultiUser   bool
		wantUnboundProj bool // pinned downstream consequence: unbound session +
		// maxScope=project must recall NOTHING project-scoped (global +
		// own-user rows still recallable); asserted via empty projectID +
		// narrow maxScope tuple.
	}{
		{
			name:          "empty MemoryMaxScope defaults to project",
			spec:          spec(""),
			projectID:     "proj-a",
			userID:        "",
			wantMax:       memory.ScopeProject,
			wantProjectID: "proj-a",
			wantUserID:    "",
			wantMultiUser: false,
		},
		{
			name:          "user maxScope honored",
			spec:          spec("user"),
			projectID:     "proj-a",
			userID:        "user-u1",
			wantMax:       memory.ScopeUser,
			wantProjectID: "proj-a",
			wantUserID:    "user-u1",
			wantMultiUser: true,
		},
		{
			name:          "global maxScope honored (chief-of-staff pattern)",
			spec:          spec("global"),
			projectID:     "proj-a",
			userID:        "user-u1",
			wantMax:       memory.ScopeGlobal,
			wantProjectID: "proj-a",
			wantUserID:    "user-u1",
			wantMultiUser: true,
		},
		{
			name:          "project maxScope explicit",
			spec:          spec("project"),
			projectID:     "proj-b",
			userID:        "",
			wantMax:       memory.ScopeProject,
			wantProjectID: "proj-b",
			wantUserID:    "",
			wantMultiUser: false,
		},
		{
			name:          "invalid MemoryMaxScope fails SAFE to project (never global)",
			spec:          spec("bogus"),
			projectID:     "proj-a",
			userID:        "",
			wantMax:       memory.ScopeProject,
			wantProjectID: "proj-a",
			wantUserID:    "",
			wantMultiUser: false,
		},
		{
			name:            "unbound session keeps empty projectID (never daemon CWD fallback)",
			spec:            spec("project"),
			projectID:       "",
			userID:          "",
			wantMax:         memory.ScopeProject,
			wantProjectID:   "",
			wantUserID:      "",
			wantMultiUser:   false,
			wantUnboundProj: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			maxScope, projectID, userID, multiUser := resolveScopeInputsWithCeiling(tc.spec, "", tc.projectID, tc.userID)

			if maxScope != tc.wantMax {
				t.Errorf("maxScope = %q, want %q", string(maxScope), string(tc.wantMax))
			}
			if projectID != tc.wantProjectID {
				t.Errorf("projectID = %q, want %q", projectID, tc.wantProjectID)
			}
			if userID != tc.wantUserID {
				t.Errorf("userID = %q, want %q", userID, tc.wantUserID)
			}
			if multiUser != tc.wantMultiUser {
				t.Errorf("multiUser bit = %v, want %v (Flag 3: the predicate needs this alongside userID)", multiUser, tc.wantMultiUser)
			}
			if tc.wantUnboundProj {
				// B1 consequence: an unbound session under maxScope=project
				// must not recall anything project-scoped. The tuple is the
				// pin: empty projectID + project max.
				if projectID == "" && maxScope == memory.ScopeProject {
					return // exactly the fail-isolated tuple
				}
				t.Errorf("unbound-session tuple (max=%q, project=%q) does not isolate project rows", string(maxScope), projectID)
			}
		})
	}
}
