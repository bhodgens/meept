package session

import "testing"

// TestResolveWorkingDir_Precedence pins the repo-wide working-directory
// precedence (AGENTS.md): WorktreePath > ProjectPath > DetectionContext.CWD.
func TestResolveWorkingDir_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		sess     *Session
		wantDir  string
		wantFrom WorkingDirSource
	}{
		{
			name:     "nil session",
			sess:     nil,
			wantDir:  "",
			wantFrom: WorkingDirFromNone,
		},
		{
			name:     "nothing bound",
			sess:     &Session{ID: "session-a"},
			wantDir:  "",
			wantFrom: WorkingDirFromNone,
		},
		{
			name:     "project only",
			sess:     &Session{ID: "session-a", ProjectPath: "/repos/proj"},
			wantDir:  "/repos/proj",
			wantFrom: WorkingDirFromProject,
		},
		{
			name:     "detection cwd only (no project)",
			sess:     &Session{ID: "session-a", DetectionContext: &DetectionContext{CWD: "/home/user/code"}},
			wantDir:  "/home/user/code",
			wantFrom: WorkingDirFromDetection,
		},
		{
			name: "worktree beats project",
			sess: &Session{
				ID:           "session-a",
				WorktreePath: "/wt/phase-1",
				ProjectPath:  "/repos/proj",
			},
			wantDir:  "/wt/phase-1",
			wantFrom: WorkingDirFromWorktree,
		},
		{
			name: "worktree beats project and detection",
			sess: &Session{
				ID:               "session-a",
				WorktreePath:     "/wt/phase-1",
				ProjectPath:      "/repos/proj",
				DetectionContext: &DetectionContext{CWD: "/home/user/code"},
			},
			wantDir:  "/wt/phase-1",
			wantFrom: WorkingDirFromWorktree,
		},
		{
			name: "project beats detection cwd",
			sess: &Session{
				ID:               "session-a",
				ProjectPath:      "/repos/proj",
				DetectionContext: &DetectionContext{CWD: "/home/user/code"},
			},
			wantDir:  "/repos/proj",
			wantFrom: WorkingDirFromProject,
		},
		{
			name: "empty detection context cwd is not a binding",
			sess: &Session{
				ID:               "session-a",
				DetectionContext: &DetectionContext{DetectedProjectID: "proj-1"},
			},
			wantDir:  "",
			wantFrom: WorkingDirFromNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDir, gotFrom := ResolveWorkingDir(tt.sess)
			if gotDir != tt.wantDir {
				t.Errorf("ResolveWorkingDir() dir = %q, want %q", gotDir, tt.wantDir)
			}
			if gotFrom != tt.wantFrom {
				t.Errorf("ResolveWorkingDir() source = %q, want %q", gotFrom, tt.wantFrom)
			}
		})
	}
}
