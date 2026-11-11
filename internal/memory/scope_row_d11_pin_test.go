package memory

// Pin test for the bughunt D11 exported-scope-seam additions (RowOwner +
// VisibleScopeRow): the one-row C2 predicate must agree with the
// committed filterScopeResults matrix. TESTS ONLY.

import "testing"

func TestVisibleScopeRowMatchesC2Matrix(t *testing.T) {
	projectP1 := map[string]any{"scope": "project", "project_id": "p1"}
	projectP2 := map[string]any{"scope": "project", "project_id": "p2"}
	userU1 := map[string]any{"scope": "user", "user_id": "u1"}
	global := map[string]any{"scope": "global"}

	cases := []struct {
		name      string
		meta      map[string]any
		maxScope  Scope
		projectID string
		userID    string
		multiUser bool
		want      bool
	}{
		{"global row always visible", global, ScopeGlobal, "", "", false, true},
		{"project caller sees own project row", projectP1, ScopeProject, "p1", "u1", false, true},
		{"project caller cannot see foreign project row", projectP2, ScopeProject, "p1", "u1", false, false},
		{"project caller unbound sees nothing project-scoped", projectP1, ScopeProject, "", "u1", false, false},
		{"user caller spans projects", projectP2, ScopeUser, "", "u1", false, true},
		{"user caller sees own user row", userU1, ScopeUser, "", "u1", false, true},
		{"user caller cannot see foreign user row", userU1, ScopeUser, "", "u2", false, false},
		{"multiuser empty userID fails closed", userU1, ScopeUser, "", "", true, false},
		{"single-user empty userID matches", userU1, ScopeUser, "", "", false, true},
		{"missing scope stamp reads global", map[string]any{}, ScopeGlobal, "", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := VisibleScopeRow(tc.meta, tc.maxScope, tc.projectID, tc.userID, tc.multiUser)
			if got != tc.want {
				t.Fatalf("VisibleScopeRow = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRowOwner(t *testing.T) {
	meta := map[string]any{"project_id": "p1", "user_id": "u1"}
	if got := RowOwner(meta, "project_id"); got != "p1" {
		t.Fatalf("RowOwner(project_id) = %q, want p1", got)
	}
	if got := RowOwner(nil, "user_id"); got != "" {
		t.Fatalf("RowOwner(nil) = %q, want empty", got)
	}
	if got := RowOwner(map[string]any{"project_id": 42}, "project_id"); got != "" {
		t.Fatalf("RowOwner(non-string) = %q, want empty", got)
	}
}
