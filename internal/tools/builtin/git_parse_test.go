package builtin

import (
	"context"
	"strings"
	"testing"
)

// TestParseSimpleStatus_HandlesSpacesInPaths verifies that
// GitSplitTool.parseSimpleStatus correctly parses file paths that contain
// spaces. Regression test for round-5 S4-5: strings.Fields split on
// whitespace, truncating paths like "my file.go" to "my".
func TestParseSimpleStatus_HandlesSpacesInPaths(t *testing.T) {
	tool := &GitSplitTool{}

	tests := []struct {
		name       string
		input      string
		wantPaths  []string
		wantStatus []string
	}{
		{
			name:       "single modified file with space",
			input:      " M my file.go",
			wantPaths:  []string{"my file.go"},
			wantStatus: []string{"modified"},
		},
		{
			name:       "added file with multiple spaces",
			input:      "A  some other path.go",
			wantPaths:  []string{"some other path.go"},
			wantStatus: []string{"added"},
		},
		{
			name:       "deleted file with space",
			input:      "D  gone file.go",
			wantPaths:  []string{"gone file.go"},
			wantStatus: []string{"deleted"},
		},
		{
			name:       "renamed file with tab and space",
			input:      "R  old path.go\tnew path.go",
			wantPaths:  []string{"new path.go"},
			wantStatus: []string{"renamed"},
		},
		{
			name: "multiple files mixed",
			input: " M normal.go\n" +
				"A  with space.go\n" +
				"D  gone now.go",
			wantPaths:  []string{"normal.go", "with space.go", "gone now.go"},
			wantStatus: []string{"modified", "added", "deleted"},
		},
		{
			name:      "empty line skipped",
			input:     "\n\n M ok.go\n",
			wantPaths: []string{"ok.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := tool.parseSimpleStatus(tt.input)
			if len(changes) != len(tt.wantPaths) {
				t.Fatalf("got %d changes, want %d (changes=%+v)", len(changes), len(tt.wantPaths), changes)
			}
			for i, want := range tt.wantPaths {
				if changes[i].FilePath != want {
					t.Errorf("changes[%d].FilePath = %q, want %q", i, changes[i].FilePath, want)
				}
			}
			if tt.wantStatus != nil {
				for i, want := range tt.wantStatus {
					if changes[i].Status != want {
						t.Errorf("changes[%d].Status = %q, want %q", i, changes[i].Status, want)
					}
				}
			}
		})
	}
}

// TestParseFileStatus_HandlesSpacesInPaths verifies that
// GitOverviewTool.parseFileStatus correctly handles paths with spaces.
func TestParseFileStatus_HandlesSpacesInPaths(t *testing.T) {
	tool := &GitOverviewTool{}

	// parseFileStatus calls getFileStats which runs git; we can't run git here
	// but we can verify the parsing portion by capturing the FileChangeInfo
	// entries. We set up a stub that returns zeros via the git command failing.
	// Since getFileStats returns 0,0 on git failure, we only check FilePath.

	tests := []struct {
		name          string
		input         string
		wantPaths     []string
		wantPrevPaths []string // for renames
	}{
		{
			name:      "modified file with space",
			input:     " M my file.go",
			wantPaths: []string{"my file.go"},
		},
		{
			name:          "renamed with spaces and tab",
			input:         "R  old file.go\tnew file.go",
			wantPaths:     []string{"new file.go"},
			wantPrevPaths: []string{"old file.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes, err := tool.parseFileStatus(context.Background(), "/tmp/nonexistent", tt.input, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(changes) != len(tt.wantPaths) {
				t.Fatalf("got %d changes, want %d (changes=%+v)", len(changes), len(tt.wantPaths), changes)
			}
			for i, want := range tt.wantPaths {
				if changes[i].FilePath != want {
					t.Errorf("changes[%d].FilePath = %q, want %q", i, changes[i].FilePath, want)
				}
			}
			if tt.wantPrevPaths != nil {
				for i, want := range tt.wantPrevPaths {
					if changes[i].PrevPath != want {
						t.Errorf("changes[%d].PrevPath = %q, want %q", i, changes[i].PrevPath, want)
					}
				}
			}
		})
	}
}

// TestParseSimpleStatus_LineFormat documents the porcelain=v1 line format
// handled by parseSimpleStatus and serves as an executable spec.
func TestParseSimpleStatus_LineFormat(t *testing.T) {
	tool := &GitSplitTool{}
	// Per git status --porcelain=v1, the format is "XY PATH" where XY is 2
	// chars (index/worktree status) followed by a space and the path. For
	// renames, the format is "XY ORIG\tNEW".
	lines := []string{
		" M modified.go",        // worktree modified, unstaged
		"M  staged.go",          // staged modification
		"MM both.go",            // staged and unstaged modifications
		"?? untracked.go",       // untracked
		"A  added.go",           // added
		"R  old.go\trenamed.go", // renamed
		"D  deleted.go",         // deleted
		" M with space.go",      // path with space
	}
	input := strings.Join(lines, "\n")
	changes := tool.parseSimpleStatus(input)
	if len(changes) != len(lines) {
		t.Fatalf("got %d changes, want %d", len(changes), len(lines))
	}
	// Spot-check the renamed and with-space entries (the bug surface).
	wantRenamed := "renamed.go"
	if changes[5].FilePath != wantRenamed {
		t.Errorf("renamed FilePath = %q, want %q", changes[5].FilePath, wantRenamed)
	}
	if changes[5].Status != "renamed" {
		t.Errorf("renamed Status = %q, want %q", changes[5].Status, "renamed")
	}
	wantSpace := "with space.go"
	if changes[7].FilePath != wantSpace {
		t.Errorf("with-space FilePath = %q, want %q", changes[7].FilePath, wantSpace)
	}
}
