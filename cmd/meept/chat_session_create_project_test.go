package main

import (
	"testing"
)

// M5 regression pin: `meept chat --session fresh --session-create --project
// myapp --cwd /other` must bind the session to the RESOLVED project, not the
// raw --project flag name. runChat resolves --project into a project ID and
// local path (resolveProjectByName); the --session-create path must thread
// BOTH of those values into session.create. The daemon looks up project_id by
// ID, so passing the name silently loses the binding and leaves the session
// bound to the CWD.
//
// Seam limitation: runChat calls connectDaemon() directly with no injection
// hook (see backup_cmd_test.go's note), so a full runChat-level test is not
// practical. This pins the boundary where the resolved values are threaded
// into the create path (chatWithSessionWithProject -> session.create) using
// the package's existing fakeAsyncClient RPC seam. await=off keeps the test
// deterministic: session.create fires before chat.submit, so no terminal
// event needs to be scripted.
func TestSessionCreateCarriesResolvedProjectIDAndPath(t *testing.T) {
	f := newChatWiringFake(nil)
	// Missing session -> the --session-create branch fires.
	f.responses["session.get"] = func(params any) (any, error) {
		return map[string]any{"error": "session not found"}, nil
	}
	f.responses["session.create"] = func(params any) (any, error) {
		m, _ := params.(map[string]any)
		name, _ := m["name"].(string)
		return map[string]any{"id": name, "name": name}, nil
	}

	oldCreate, oldAwait, oldProject := chatSessionCreate, chatAwait, chatProject
	chatSessionCreate = true
	chatAwait = "off"
	// The raw flag name deliberately differs from the resolved ID: a
	// regression to projectIDForCreate() (which returned chatProject) fails.
	chatProject = "myapp"
	t.Cleanup(func() {
		chatSessionCreate, chatAwait, chatProject = oldCreate, oldAwait, oldProject
	})

	const resolvedID = "proj-id-abc123"
	const resolvedPath = "/srv/myapp"
	if err := chatWithSessionWithProject(f, "fresh-sess", "hello", resolvedID, resolvedPath); err != nil {
		t.Fatalf("chatWithSessionWithProject: %v", err)
	}

	var created bool
	var createParams map[string]any
	for i, method := range f.methods {
		if method == "session.create" {
			created = true
			createParams, _ = f.params[i].(map[string]any)
		}
	}
	if !created {
		t.Fatal("session.create never fired on the --session-create path")
	}
	if got := createParams["project_id"]; got != resolvedID {
		t.Errorf("session.create project_id = %v, want resolved ID %q (not the raw --project flag name %q)", got, resolvedID, chatProject)
	}
	if got := createParams["project_path"]; got != resolvedPath {
		t.Errorf("session.create project_path = %v, want resolved path %q", got, resolvedPath)
	}
}

// TestSessionCreateOmitsProjectWhenUnset pins the no-project case: without a
// resolved binding the create request carries neither field, so the daemon
// falls back to its per-session CWD contract rather than a bogus binding.
func TestSessionCreateOmitsProjectWhenUnset(t *testing.T) {
	f := newChatWiringFake(nil)
	f.responses["session.get"] = func(params any) (any, error) {
		return map[string]any{"error": "session not found"}, nil
	}
	f.responses["session.create"] = func(params any) (any, error) {
		m, _ := params.(map[string]any)
		name, _ := m["name"].(string)
		return map[string]any{"id": name, "name": name}, nil
	}

	oldCreate, oldAwait := chatSessionCreate, chatAwait
	chatSessionCreate = true
	chatAwait = "off"
	t.Cleanup(func() { chatSessionCreate, chatAwait = oldCreate, oldAwait })

	if err := chatWithSessionWithProject(f, "fresh-sess", "hello", "", ""); err != nil {
		t.Fatalf("chatWithSessionWithProject: %v", err)
	}

	var createParams map[string]any
	for i, method := range f.methods {
		if method == "session.create" {
			createParams, _ = f.params[i].(map[string]any)
		}
	}
	if createParams == nil {
		t.Fatal("session.create never fired on the --session-create path")
	}
	if _, has := createParams["project_id"]; has {
		t.Errorf("session.create must omit project_id when no project resolved; params=%v", createParams)
	}
	if _, has := createParams["project_path"]; has {
		t.Errorf("session.create must omit project_path when no project resolved; params=%v", createParams)
	}
}
