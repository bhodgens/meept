package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// newTestSpillCompactor builds a ContextCompactor with spill enabled and a
// temp MEEPT_HOME. Returns the compactor and the spill root it will use.
func newTestSpillCompactor(t *testing.T, spillEnabled bool) (*ContextCompactor, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	cfg := DefaultCompactorConfig()
	cfg.SpillEnabled = spillEnabled
	c := NewContextCompactor(cfg, nil, nil, nil)
	return c, filepath.Join(home, "spill")
}

func newSpillPruneMessages(oldContent string) []ChatMessage {
	recentOutput := makeLongString(39000)
	return []ChatMessage{
		{Role: RoleUser, Content: "do old work"},
		{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
			{ID: "tc_old", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: oldContent, ToolCallID: "tc_old"},
		{Role: RoleUser, Content: "do recent work"},
		{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
			{ID: "tc_recent", Function: ToolCallFunction{Name: "shell_execute", Arguments: `{}`}},
		}},
		{Role: RoleTool, Content: recentOutput, ToolCallID: "tc_recent"},
	}
}

func TestSpillServiceWritesAndReturnsPointer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	svc := NewSpillService("")
	content := strings.Repeat("tool output line\n", 300) // ~1800 tokens

	pointer, err := svc.Spill(context.Background(), "session-abc123", content)
	if err != nil {
		t.Fatalf("Spill returned error: %v", err)
	}

	wantPath := filepath.Join(home, "spill", "session-abc123")
	sum := sha256.Sum256([]byte(content))
	wantFile := filepath.Join(wantPath, hex.EncodeToString(sum[:])[:16]+".txt")

	// File exists on disk with the raw (unwrapped) content.
	got, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatalf("spill file not written: %v", err)
	}
	if string(got) != content {
		t.Error("spill file content must be the raw, unwrapped tool output")
	}

	// File mode 0600, dir mode 0700.
	info, err := os.Stat(wantFile)
	if err != nil {
		t.Fatalf("stat spill file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("spill file perm = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat spill dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf("spill dir perm = %o, want 700", perm)
	}

	// Exact pointer line with absolute path and token count (HeuristicTokenizer: 3 chars/token).
	tokens := (len(content) + 2) / 3
	want := fmt.Sprintf("[Output spilled to %s — re-read with the file tool if needed (%d tokens)]", wantFile, tokens)
	if pointer != want {
		t.Errorf("pointer line = %q, want %q", pointer, want)
	}
	if !filepath.IsAbs(wantFile) {
		t.Error("pointer path must be absolute")
	}
}

func TestSpillServiceSanitizesSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	svc := NewSpillService("")
	hostile := "../../etc/passwd"

	pointer, err := svc.Spill(context.Background(), hostile, "content")
	if err != nil {
		t.Fatalf("Spill returned error: %v", err)
	}

	// Path must stay under the spill root; traversal components stripped.
	if strings.Contains(pointer, "..") {
		t.Errorf("pointer path escaped spill root: %q", pointer)
	}
	wantDir := filepath.Join(home, "spill", hostilesanitized(hostile))
	gotPath := pointer[len("[Output spilled to "):]
	if idx := strings.Index(gotPath, " — re-read"); idx >= 0 {
		gotPath = gotPath[:idx]
	}
	if !strings.HasPrefix(gotPath, wantDir+string(filepath.Separator)) {
		t.Errorf("pointer = %q, want it under %q", pointer, wantDir)
	}
}

func hostilesanitized(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func TestSpillDisabledReturnsTruncation(t *testing.T) {
	c, spillRoot := newTestSpillCompactor(t, false)
	largeOutput := makeLongString(30000)

	msgs := newSpillPruneMessages(largeOutput)
	result := c.pruneToolOutputs(msgs)

	// Old output replaced with the EXACT legacy truncation text — byte-identical
	// behavior when spill is disabled.
	stub := fmt.Sprintf("[Output truncated — %d tokens saved]", 30000)
	want := fmt.Sprintf("[Output truncated — %d tokens saved]", 30000-c.tokenizer.CountTokens(stub))
	if got := result[2].Content; got != want {
		t.Errorf("disabled spill should keep legacy truncation text, got: %q", got)
	}
	// Nothing spilled.
	if _, err := os.Stat(spillRoot); !os.IsNotExist(err) {
		t.Errorf("spill dir must not be created when spill disabled (stat err: %v)", err)
	}
}

func TestSpillWriteFailureFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	// Make $MEEPT_HOME read-only so MkdirAll for spill/<session> fails.
	if err := os.Chmod(home, 0500); err != nil {
		t.Fatalf("chmod home: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0700) })

	cfg := DefaultCompactorConfig()
	cfg.SpillEnabled = true
	c := NewContextCompactor(cfg, nil, nil, nil)

	largeOutput := makeLongString(30000)
	msgs := newSpillPruneMessages(largeOutput)

	// Must NOT fail the turn: falls back to truncation text, no error out.
	result := c.pruneToolOutputs(msgs)
	if got := result[2].Content; !strings.Contains(got, "Output truncated") {
		t.Errorf("spill write failure should fall back to truncation text, got: %q", got)
	}
	if got := result[2].Content; strings.Contains(got, "Output spilled") {
		t.Errorf("spill pointer must not appear when the spill write failed, got: %q", got)
	}
}

func TestSpillEnabledPrunedOutputUsesPointer(t *testing.T) {
	c, spillRoot := newTestSpillCompactor(t, true)
	largeOutput := makeLongString(30000)

	msgs := newSpillPruneMessages(largeOutput)
	result := c.pruneToolOutputs(msgs)

	got := result[2].Content
	if !strings.HasPrefix(got, "[Output spilled to ") || !strings.Contains(got, "re-read with the file tool if needed") {
		t.Errorf("spill-enabled prune should use the pointer line, got: %q", got)
	}

	// Pointer path must point at the real spill file containing the raw output.
	start := len("[Output spilled to ")
	end := strings.Index(got, " — re-read")
	if end < 0 {
		t.Fatalf("pointer line malformed: %q", got)
	}
	path := got[start:end]
	if !filepath.IsAbs(path) || !strings.HasPrefix(path, spillRoot+string(filepath.Separator)) {
		t.Fatalf("pointer path %q not under spill root %q", path, spillRoot)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file missing: %v", err)
	}
	if string(data) != largeOutput {
		t.Error("spilled content must be the raw tool output")
	}
	if strings.Contains(got, "Output truncated") {
		t.Error("pointer line must not contain the truncation text when spill succeeded")
	}

	// Recent protected output untouched.
	if result[5].Content != newSpillPruneMessages(largeOutput)[5].Content {
		t.Error("recent tool output within protected budget should not be pruned")
	}
}

// --- Spill sweep tests (token-efficiency branch 01, leaf 02; gate T4 contract) ---

// sweepTestFile creates a spill fixture file at root/session/name of size
// bytes with mtime set to now.Add(age).
func sweepTestFile(t *testing.T, root, session, name string, size int, age time.Duration, now time.Time) string {
	t.Helper()
	dir := filepath.Join(root, session)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	if err := os.Chtimes(p, now.Add(age), now.Add(age)); err != nil {
		t.Fatalf("chtimes %s: %v", p, err)
	}
	return p
}

// sweepFileNames lists the files that exist under root (relative to root,
// slash-separated, sorted), for exact-set assertions.
func sweepFileNames(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, filepath.ToSlash(path[len(root):]))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(names)
	return names
}

// sweepRemainingBytes sums the sizes of all files under root.
func sweepRemainingBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, statErr := d.Info()
			if statErr != nil {
				return statErr
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return total
}

// TestSpillSweepRemovesFilesOlderThan24h pins the age branch of the sweep:
// files older than now-24h are removed; newer files stay — including a file
// that would be size-evicted (the tree is over the cap) but is inside the 1h
// in-flight guard.
func TestSpillSweepRemovesFilesOlderThan24h(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cap := int64(2) << 20 // 2 MiB — the full tree (4 MiB) exceeds the cap,
	// but after the age sweep removes both stale files only the 1 MiB
	// fresh file remains, so the size branch is a no-op and the 5h file
	// is asserted to survive the age branch alone.
	stale := sweepTestFile(t, root, "s-old", "old-48h.txt", 2<<20, -48*time.Hour, now)
	_ = sweepTestFile(t, root, "s-mid", "mid-30h.txt", 1<<20, -30*time.Hour, now)
	sweepTestFile(t, root, "s-new", "fresh-5h.txt", 1<<20, -5*time.Hour, now)

	removed, err := SweepToolSpills(root, 24*time.Hour, cap, now)
	if err != nil {
		t.Fatalf("SweepToolSpills returned error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2 (the 48h and 30h files)", removed)
	}
	if _, statErr := os.Stat(stale); !os.IsNotExist(statErr) {
		t.Errorf("48h-old file must be removed (stat err: %v)", statErr)
	}
	if got := sweepFileNames(t, root); len(got) != 1 || got[0] != "/s-new/fresh-5h.txt" {
		t.Errorf("surviving files = %v, want only the 5h file (spared by the age branch; the post-age tree is under the cap)", got)
	}
}

// TestSpillSweepEvictsOldestPast512MB pins the size-cap branch: when the
// remaining tree exceeds the cap, the oldest-eligible files are evicted
// first until the cap is met. All fixtures are younger than the 24h age
// cutoff (and older than the 1h in-flight guard) so only the size branch
// fires.
func TestSpillSweepEvictsOldestPast512MB(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cap := int64(512) << 20 // 512 MiB
	// Four 200 MiB fixtures = 800 MiB, mtimes 2h/4h/6h/8h ago. Oldest-first
	// eviction removes the 8h and 6h files (800 -> 600 -> 400 MiB, the last
	// step crossing the cap); the two newest (2h, 4h) survive.
	_ = sweepTestFile(t, root, "s-a", "a-2h.txt", 200<<20, -2*time.Hour, now)
	_ = sweepTestFile(t, root, "s-b", "b-4h.txt", 200<<20, -4*time.Hour, now)
	evicted6h := sweepTestFile(t, root, "s-c", "c-6h.txt", 200<<20, -6*time.Hour, now)
	evicted8h := sweepTestFile(t, root, "s-d", "d-8h.txt", 200<<20, -8*time.Hour, now)

	removed, err := SweepToolSpills(root, 24*time.Hour, cap, now)
	if err != nil {
		t.Fatalf("SweepToolSpills returned error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2 (the 8h and 6h files, oldest first)", removed)
	}
	for _, p := range []string{evicted6h, evicted8h} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("file %s must be evicted oldest-first (stat err: %v)", p, statErr)
		}
	}
	if got := sweepFileNames(t, root); len(got) != 2 || got[0] != "/s-a/a-2h.txt" || got[1] != "/s-b/b-4h.txt" {
		t.Errorf("surviving files = %v, want the two newest (2h, 4h)", got)
	}
	if got := sweepRemainingBytes(t, root); got > cap {
		t.Errorf("remaining total = %d bytes, want <= %d", got, cap)
	}
}

// TestSpillSweepInFlightGuardSurvivesSizeEviction pins the 1h guard under
// the size branch: a tree over the cap evicts everything except the file
// whose mtime is within 1h of now, which survives even though it is the
// largest file. The 30h file dies in the age pass; the 20h file (size-only
// branch) dies in the size pass; the 30m in-flight file must survive the
// size pass even though it keeps the tree far over the cap.
func TestSpillSweepInFlightGuardSurvivesSizeEviction(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cap := int64(1) << 20
	_ = sweepTestFile(t, root, "s-a", "a-30h.txt", 512<<20, -30*time.Hour, now)
	_ = sweepTestFile(t, root, "s-b", "b-20h.txt", 512<<20, -20*time.Hour, now)
	inFlight := sweepTestFile(t, root, "s-c", "c-30m.txt", 512<<20, -30*time.Minute, now)

	removed, err := SweepToolSpills(root, 24*time.Hour, cap, now)
	if err != nil {
		t.Fatalf("SweepToolSpills returned error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2 (the 30h file via age, the 20h file via size; the 30m file is in-flight)", removed)
	}
	if _, statErr := os.Stat(inFlight); statErr != nil {
		t.Errorf("in-flight (30m) file must never be deleted under any branch: %v", statErr)
	}
	if got := sweepFileNames(t, root); len(got) != 1 || got[0] != "/s-c/c-30m.txt" {
		t.Errorf("surviving files = %v, want only the in-flight file", got)
	}
}

// TestSpillSweepDeterministicOrder pins the (mtime, path) ordering: equal
// mtimes break ties lexicographically by path, and size eviction deletes in
// that exact order. All fixtures share one mtime (5h: inside the guard
// window's exclusion, outside the age cutoff).
func TestSpillSweepDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cap := int64(1) << 20 // exactly one 1 MiB file fits
	a := sweepTestFile(t, root, "s-x", "a.txt", 1<<20, -5*time.Hour, now)
	b := sweepTestFile(t, root, "s-x", "b.txt", 1<<20, -5*time.Hour, now)
	c := sweepTestFile(t, root, "s-y", "c.txt", 1<<20, -5*time.Hour, now)
	d := sweepTestFile(t, root, "s-y", "d.txt", 1<<20, -5*time.Hour, now)

	removed, err := SweepToolSpills(root, 24*time.Hour, cap, now)
	if err != nil {
		t.Fatalf("SweepToolSpills returned error: %v", err)
	}
	// 4 MiB total, cap 1 MiB: evict a, b, c (path order within the equal
	// mtime); stop at exactly 1 MiB with d surviving.
	if removed != 3 {
		t.Fatalf("removed = %d, want 3 (a.txt, b.txt, c.txt: path tie-break within one mtime)", removed)
	}
	for _, p := range []string{a, b, c} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s must be evicted in (mtime, path) order (stat err: %v)", p, statErr)
		}
	}
	if _, statErr := os.Stat(d); statErr != nil {
		t.Errorf("d.txt must survive (newest path entry, total now == cap): %v", statErr)
	}
}

// TestDefaultToolSpillRoot pins the MEEPT_HOME-aware root resolution: the env
// override wins over the user home, and the result is <home>/spill/tools.
func TestDefaultToolSpillRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	want := filepath.Join(home, "spill", "tools")
	if got := DefaultToolSpillRoot(); got != want {
		t.Fatalf("DefaultToolSpillRoot() = %q, want %q", got, want)
	}

	// No env: falls back to the user-home-dir resolution.
	t.Setenv("MEEPT_HOME", "")
	uh, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}
	got := DefaultToolSpillRoot()
	if want := filepath.Join(uh, ".meept", "spill", "tools"); got != want && got != filepath.Join(".meept", "spill", "tools") {
		t.Fatalf("DefaultToolSpillRoot() without MEEPT_HOME = %q, want under %s or .meept", got, filepath.Join(uh, ".meept"))
	}
}
