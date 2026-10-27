package builtin

// NOTE: gate test from the github-pr-ci tree (leaf 00T, Task 2). Verifies the
// C2 read-only contract: exactly {github_pr_status, github_ci_wait,
// github_issue_get} are read-only; the five mutating tools are not.
// Registry membership only — no Execute, no network.

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/tools"
)

func TestReadOnlyFlags(t *testing.T) {
	pool := newRegisterPool(t, "")
	reg := tools.NewRegistry(slog.New(slog.DiscardHandler))
	opts := githubsvc.RegistrationOptions{
		Pool:         pool,
		DefaultOwner: "octo",
		DefaultRepo:  "cat",
		WorkingDir:   t.TempDir(),
	}
	if err := githubsvc.RegisterGitHubTools(reg, opts); err != nil {
		t.Fatalf("RegisterGitHubTools: %v", err)
	}

	readOnly := map[string]bool{
		"github_pr_status": true,
		"github_ci_wait":   true,
		"github_issue_get": true,
	}

	for _, name := range expectedGitHubToolNames {
		tool := reg.Get(name)
		if tool == nil {
			t.Fatalf("registry missing tool %q; registered=%v", name, reg.Names())
		}
		got := tool.IsReadOnly(map[string]any{})
		want := readOnly[name]
		if got != want {
			t.Errorf("%s IsReadOnly = %v, want %v", name, got, want)
		}
	}

	if len(readOnly) != 3 {
		t.Errorf("read-only set has %d entries, want exactly 3", len(readOnly))
	}
	if len(expectedGitHubToolNames) != 8 {
		t.Errorf("expected GitHub tool set has %d entries, want exactly 8", len(expectedGitHubToolNames))
	}
}
