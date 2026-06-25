package lifecycle

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestVersionerSnapshotCreatesBundle verifies that Snapshot on an existing
// skill creates versions/v1/SKILL.md and versions/v1/bundle.json and returns a
// non-empty tree SHA-256.
func TestVersionerSnapshotCreatesBundle(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	content := "---\nname: skill-a\ndescription: a\n---\n\nbody v1\n"
	skillPath := filepath.Join(dir, "skill-a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	treeSHA, err := v.Snapshot("skill-a")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if treeSHA == "" {
		t.Fatal("Snapshot returned empty tree SHA for existing skill")
	}

	// Verify SKILL.md copy exists.
	versionedSkill := filepath.Join(dir, "skill-a", "versions", "v1", "SKILL.md")
	data, err := os.ReadFile(versionedSkill)
	if err != nil {
		t.Fatalf("versioned SKILL.md missing: %v", err)
	}
	if string(data) != content {
		t.Errorf("versioned SKILL.md content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}

	// Verify bundle.json manifest exists and parses.
	manifestPath := filepath.Join(dir, "skill-a", "versions", "v1", "bundle.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("bundle.json missing: %v", err)
	}
	var entry VersionEntry
	if err := parseJSONForTest(manifestBytes, &entry); err != nil {
		t.Fatalf("parse bundle.json: %v", err)
	}
	if entry.Version != 1 {
		t.Errorf("bundle version = %d, want 1", entry.Version)
	}
	if entry.ContentSHA == "" {
		t.Error("bundle content_sha is empty")
	}
	if entry.TreeSHA256 != treeSHA {
		t.Errorf("bundle tree_sha256 = %q, Snapshot returned %q", entry.TreeSHA256, treeSHA)
	}
	if entry.Action != "snapshot" {
		t.Errorf("bundle action = %q, want %q", entry.Action, "snapshot")
	}
}

// TestVersionerSnapshotNonExistent verifies that Snapshot returns empty string
// and nil error when the skill doesn't exist yet (first-write case).
func TestVersionerSnapshotNonExistent(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	treeSHA, err := v.Snapshot("never-existed")
	if err != nil {
		t.Fatalf("Snapshot on non-existent skill returned error: %v", err)
	}
	if treeSHA != "" {
		t.Errorf("Snapshot on non-existent skill returned %q, want empty", treeSHA)
	}
}

// TestVersionerSnapshotIncrementsVersion verifies that successive Snapshot
// calls produce v1, v2, v3 in order.
func TestVersionerSnapshotIncrementsVersion(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	skillPath := filepath.Join(dir, "skill-a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for i := 1; i <= 3; i++ {
		content := "---\nname: skill-a\ndescription: a\n---\n\nbody v" + itoa(i) + "\n"
		if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile iteration %d: %v", i, err)
		}

		treeSHA, err := v.Snapshot("skill-a")
		if err != nil {
			t.Fatalf("Snapshot iteration %d: %v", i, err)
		}
		if treeSHA == "" {
			t.Fatalf("Snapshot iteration %d returned empty tree SHA", i)
		}

		// Verify version dir exists.
		versionDir := filepath.Join(dir, "skill-a", "versions", "v"+itoa(i))
		if _, err := os.Stat(filepath.Join(versionDir, "SKILL.md")); err != nil {
			t.Errorf("iteration %d: versioned SKILL.md missing: %v", i, err)
		}
		if _, err := os.Stat(filepath.Join(versionDir, "bundle.json")); err != nil {
			t.Errorf("iteration %d: bundle.json missing: %v", i, err)
		}
	}
}

// TestVersionerHistory verifies that History returns entries in version order
// after multiple snapshots.
func TestVersionerHistory(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	skillPath := filepath.Join(dir, "skill-a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for i := 1; i <= 3; i++ {
		content := "---\nname: skill-a\n---\n\nv" + itoa(i) + "\n"
		if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile v%d: %v", i, err)
		}
		if _, err := v.Snapshot("skill-a"); err != nil {
			t.Fatalf("Snapshot v%d: %v", i, err)
		}
	}

	entries, err := v.History("skill-a")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("History returned %d entries, want 3", len(entries))
	}

	for i, entry := range entries {
		wantVersion := i + 1
		if entry.Version != wantVersion {
			t.Errorf("entry %d: Version = %d, want %d", i, entry.Version, wantVersion)
		}
		if entry.ContentSHA == "" {
			t.Errorf("entry %d: ContentSHA is empty", i)
		}
		if entry.TreeSHA256 == "" {
			t.Errorf("entry %d: TreeSHA256 is empty", i)
		}
	}
}

// TestVersionerHistoryEmpty verifies that History returns an empty (non-nil)
// slice when no versions exist.
func TestVersionerHistoryEmpty(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	entries, err := v.History("no-versions")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if entries == nil {
		t.Fatal("History returned nil slice, want empty slice")
	}
	if len(entries) != 0 {
		t.Fatalf("History returned %d entries, want 0", len(entries))
	}
}

// TestVersionerRestore verifies that Restore reverts SKILL.md to a prior
// version's content.
func TestVersionerRestore(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	skillPath := filepath.Join(dir, "skill-a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write v1 content, snapshot.
	v1Content := "---\nname: skill-a\n---\n\noriginal body\n"
	if err := os.WriteFile(skillPath, []byte(v1Content), 0o644); err != nil {
		t.Fatalf("WriteFile v1: %v", err)
	}
	if _, err := v.Snapshot("skill-a"); err != nil {
		t.Fatalf("Snapshot v1: %v", err)
	}

	// Overwrite with v2 content, snapshot.
	v2Content := "---\nname: skill-a\n---\n\nmodified body\n"
	if err := os.WriteFile(skillPath, []byte(v2Content), 0o644); err != nil {
		t.Fatalf("WriteFile v2: %v", err)
	}
	if _, err := v.Snapshot("skill-a"); err != nil {
		t.Fatalf("Snapshot v2: %v", err)
	}

	// Verify current is v2.
	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile current: %v", err)
	}
	if string(got) != v2Content {
		t.Errorf("current content mismatch before restore")
	}

	// Restore to v1.
	if err := v.Restore("skill-a", 1); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify current is now v1.
	got, err = os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile after restore: %v", err)
	}
	if string(got) != v1Content {
		t.Errorf("after restore content mismatch:\ngot:  %q\nwant: %q", string(got), v1Content)
	}
}

// TestVersionerRestoreInvalidVersion verifies that Restore with version 0 or
// negative returns an error.
func TestVersionerRestoreInvalidVersion(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	if err := v.Restore("any", 0); err == nil {
		t.Error("Restore with version 0 should return error")
	}
	if err := v.Restore("any", -1); err == nil {
		t.Error("Restore with version -1 should return error")
	}
}

// TestVersionerRestoreMissingVersion verifies that Restore on a non-existent
// version returns an error.
func TestVersionerRestoreMissingVersion(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	skillPath := filepath.Join(dir, "skill-a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := v.Snapshot("skill-a"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Version 999 was never snapshotted.
	if err := v.Restore("skill-a", 999); err == nil {
		t.Error("Restore on non-existent version should return error")
	}
}

// TestVersionerPrune verifies that versions beyond maxVersionEntries are
// pruned (oldest removed first).
func TestVersionerPrune(t *testing.T) {
	dir := t.TempDir()
	v := NewVersioner(dir, nil)

	skillPath := filepath.Join(dir, "skill-a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Snapshot maxVersionEntries + 2 times.
	total := maxVersionEntries + 2
	for i := 1; i <= total; i++ {
		content := "---\nname: skill-a\n---\n\nv" + itoa(i) + "\n"
		if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile v%d: %v", i, err)
		}
		if _, err := v.Snapshot("skill-a"); err != nil {
			t.Fatalf("Snapshot v%d: %v", i, err)
		}
	}

	entries, err := v.History("skill-a")
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	if len(entries) != maxVersionEntries {
		t.Fatalf("after prune: History returned %d entries, want %d", len(entries), maxVersionEntries)
	}

	// The oldest versions (v1, v2) should be pruned; newest should be
	// [v3 .. v22] (maxVersionEntries=20, total=22).
	firstVersion := entries[0].Version
	if firstVersion != 3 {
		t.Errorf("after prune: first entry version = %d, want 3", firstVersion)
	}
	lastVersion := entries[len(entries)-1].Version
	if lastVersion != total {
		t.Errorf("after prune: last entry version = %d, want %d", lastVersion, total)
	}

	// Verify pruned version directories are gone.
	prunedDir := filepath.Join(dir, "skill-a", "versions", "v1")
	if _, err := os.Stat(prunedDir); err == nil {
		t.Error("pruned version dir v1 should not exist")
	}
}

// TestWriterSetVersioner verifies that SetVersioner is nil-guarded (does not
// panic on nil) and that a wired Versioner captures a snapshot before
// WriteSkill overwrites.
func TestWriterSetVersioner(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)
	v := NewVersioner(dir, nil)

	// SetVersioner with nil should not crash and should leave versioner unset.
	w.SetVersioner(nil)

	w.SetVersioner(v)

	v1Content := "---\nname: skill-b\n---\n\noriginal\n"
	if err := w.WriteSkill("skill-b", v1Content); err != nil {
		t.Fatalf("WriteSkill v1: %v", err)
	}

	v2Content := "---\nname: skill-b\n---\n\nmodified\n"
	if err := w.WriteSkill("skill-b", v2Content); err != nil {
		t.Fatalf("WriteSkill v2: %v", err)
	}

	// Versioner should have captured a snapshot of v1 before v2 overwrote it.
	entries, err := v.History("skill-b")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 version snapshot, got %d", len(entries))
	}

	// Restore to v1 and verify content matches.
	if err := v.Restore("skill-b", 1); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := w.ReadSkill("skill-b")
	if err != nil {
		t.Fatalf("ReadSkill after restore: %v", err)
	}
	if got != v1Content {
		t.Errorf("after restore content mismatch:\ngot:  %q\nwant: %q", got, v1Content)
	}
}

// TestWriterDedup verifies that writing identical content to two different
// skill names returns ErrDuplicateContent.
func TestWriterDedup(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: skill-x\n---\n\nsame body\n"
	if err := w.WriteSkill("skill-x", content); err != nil {
		t.Fatalf("WriteSkill skill-x: %v", err)
	}

	// Writing identical content under a different skill name should error.
	err := w.WriteSkill("skill-y", content)
	if err == nil {
		t.Fatal("WriteSkill skill-y with identical content should return ErrDuplicateContent")
	}
	if !isErrDuplicateContent(err) {
		t.Errorf("expected ErrDuplicateContent, got: %v", err)
	}

	// Verify skill-y was NOT written.
	if _, err := os.Stat(filepath.Join(dir, "skill-y", "SKILL.md")); err == nil {
		t.Error("skill-y should not exist after dedup rejection")
	}

	// Verify skill-x still exists.
	if _, err := os.Stat(filepath.Join(dir, "skill-x", "SKILL.md")); err != nil {
		t.Error("skill-x should still exist after dedup rejection of skill-y")
	}
}

// TestWriterDedupSameNameAllowed verifies that rewriting the SAME skill with
// the same content is allowed (not flagged as duplicate).
func TestWriterDedupSameNameAllowed(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: skill-z\n---\n\nsame name rewrite\n"
	if err := w.WriteSkill("skill-z", content); err != nil {
		t.Fatalf("WriteSkill #1: %v", err)
	}
	// Rewriting same skill with same content should succeed.
	if err := w.WriteSkill("skill-z", content); err != nil {
		t.Errorf("WriteSkill #2 (same name, same content) should succeed, got: %v", err)
	}
}

// TestWriterDedupDifferentContentAllowed verifies that writing different
// content to a second skill is allowed.
func TestWriterDedupDifferentContentAllowed(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	if err := w.WriteSkill("skill-a", "---\nname: a\n---\n\nbody a\n"); err != nil {
		t.Fatalf("WriteSkill a: %v", err)
	}
	if err := w.WriteSkill("skill-b", "---\nname: b\n---\n\nbody b\n"); err != nil {
		t.Errorf("WriteSkill b (different content) should succeed, got: %v", err)
	}
}

// TestWriterVersionerSnapshotBeforeOverwrite verifies that when a Versioner is
// wired, WriteSkill captures a snapshot of the existing content before
// overwriting (not on first create).
func TestWriterVersionerSnapshotBeforeOverwrite(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)
	v := NewVersioner(dir, nil)
	w.SetVersioner(v)

	// First write — no snapshot (skill doesn't exist yet).
	if err := w.WriteSkill("skill-a", "---\nname: a\n---\n\nv1\n"); err != nil {
		t.Fatalf("WriteSkill v1: %v", err)
	}

	entries, err := v.History("skill-a")
	if err != nil {
		t.Fatalf("History after first write: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("first write should not snapshot, got %d entries", len(entries))
	}

	// Second write — should snapshot v1 before overwriting.
	if err := w.WriteSkill("skill-a", "---\nname: a\n---\n\nv2\n"); err != nil {
		t.Fatalf("WriteSkill v2: %v", err)
	}

	entries, err = v.History("skill-a")
	if err != nil {
		t.Fatalf("History after second write: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("second write should snapshot v1, got %d entries", len(entries))
	}
}

// --- helpers ---

// itoa is a tiny strconv.Itoa wrapper to keep imports minimal in test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// parseJSONForTest wraps json.Unmarshal for tests.
func parseJSONForTest(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// isErrDuplicateContent checks whether err wraps ErrDuplicateContent.
func isErrDuplicateContent(err error) bool {
	return errors.Is(err, ErrDuplicateContent)
}
