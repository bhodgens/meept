package groups

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/caimlas/meept/internal/agents"
)

// seedAgentMD writes a minimal AGENT.md fixture and returns its path.
func seedAgentMD(t *testing.T, dir, id string, groups []string) string {
	t.Helper()
	agentDir := filepath.Join(dir, id)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fm := "---\nid: " + id + "\nname: Test " + id + "\nrole: executor\n"
	if len(groups) > 0 {
		fm += "groups: [" + joinComma(groups) + "]\n"
	}
	fm += "enabled: true\n---\n\nBody text.\n"
	p := filepath.Join(agentDir, "AGENT.md")
	if err := os.WriteFile(p, []byte(fm), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func joinComma(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func readGroupsFrontmatter(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	def, err := agents.ParseAgentText(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return def.Groups
}

// T2 PASS state (agent lane): assign/remove route agent subjects to the
// frontmatter writer; file writes pass the parse round-trip.
func TestSetAgentGroups_ReplacesAndClears(t *testing.T) {
	dir := t.TempDir()
	p := seedAgentMD(t, dir, "coder", []string{"coders"})

	// Replace.
	if err := SetAgentGroups(p, []string{"oncall", "auditors"}); err != nil {
		t.Fatalf("SetAgentGroups replace: %v", err)
	}
	got := readGroupsFrontmatter(t, p)
	if !slices.Equal(got, []string{"oncall", "auditors"}) {
		t.Fatalf("groups = %v, want [oncall auditors]", got)
	}

	// Clear.
	if err := SetAgentGroups(p, nil); err != nil {
		t.Fatalf("SetAgentGroups clear: %v", err)
	}
	if got := readGroupsFrontmatter(t, p); len(got) != 0 {
		t.Fatalf("groups after clear = %v, want empty", got)
	}
	// Body intact.
	data, _ := os.ReadFile(p)
	if !containsStr(string(data), "Body text.") {
		t.Fatalf("body lost after rewrite: %s", string(data))
	}
}

func TestSetAgentGroups_InsertsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	p := seedAgentMD(t, dir, "coder", nil)
	if err := SetAgentGroups(p, []string{"coders"}); err != nil {
		t.Fatalf("SetAgentGroups insert: %v", err)
	}
	if got := readGroupsFrontmatter(t, p); !slices.Equal(got, []string{"coders"}) {
		t.Fatalf("groups = %v, want [coders]", got)
	}
}

func TestAgentDefinitionPath_ResolvesUserTierFirst(t *testing.T) {
	home := t.TempDir()
	bundled := t.TempDir()
	// Create both; user tier wins.
	userP := seedAgentMD(t, filepath.Join(home, "agents"), "coder", nil) // home/agents/coder/AGENT.md
	bundledDir := filepath.Join(bundled)
	if err := os.MkdirAll(filepath.Join(bundledDir, "coder"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(bundledDir, "coder", "AGENT.md"), []byte("---\nid: coder\n---\nbody"), 0o644)

	got, ok := AgentDefinitionPath(home, bundledDir, "coder")
	if !ok || got != userP {
		t.Fatalf("AgentDefinitionPath = (%q,%v), want user tier %q", got, ok, userP)
	}
	// Bundled fallback.
	if _, err := os.Stat(userP); err == nil {
		_ = os.Remove(userP)
		_ = os.Remove(filepath.Dir(userP))
	}
	got, ok = AgentDefinitionPath(home, bundledDir, "coder")
	if !ok {
		t.Fatalf("bundled fallback not resolved")
	}
	// Traversal rejected.
	if _, ok := AgentDefinitionPath(home, bundledDir, "../escape"); ok {
		t.Fatalf("path traversal id accepted")
	}
}

func containsStr(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
