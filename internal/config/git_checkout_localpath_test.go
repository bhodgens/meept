package config

import (
	"log/slog"
	"testing"
)

// Pins the L14 fix: file:// URLs are LOCAL paths (go-git's file transport
// handles them), so cloneShallow must not set Depth — shipping Depth to
// the file transport fails with "reference not found".
func TestGitCheckoutIsLocalPath(t *testing.T) {
	g := func(url string) *GitCheckout {
		return &GitCheckout{repoURL: url, logger: slog.Default()}
	}
	cases := []struct {
		url  string
		want bool
	}{
		{"/absolute/path/to/repo", true},
		{"relative/path", true},
		{"file:///absolute/path/to/repo", true}, // L14: was misclassified remote
		{"file://C:/path/repo", true},
		{"https://github.com/example/repo.git", false},
		{"http://example.com/repo.git", false},
		{"git@github.com:example/repo.git", false},
		{"ssh://git@host/path/repo.git", false},
	}
	for _, tc := range cases {
		if got := g(tc.url).isLocalPath(); got != tc.want {
			t.Errorf("isLocalPath(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
