package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// TestWriterWriteSkill verifies that WriteSkill creates the expected file
// at <skillsDir>/<name>/SKILL.md.
func TestWriterWriteSkill(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: test-skill\ndescription: A test skill\n---\n\n# test-skill\n\nThis is a test.\n"
	if err := w.WriteSkill("test-skill", content); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	// Verify file exists at expected path.
	expectedPath := filepath.Join(dir, "test-skill", "SKILL.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(data) != content {
		t.Errorf("Written content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}
}

// TestWriterReadSkill verifies that ReadSkill returns the written content.
func TestWriterReadSkill(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: test-skill\ndescription: A test skill\n---\n\n# test-skill\n\nRead me.\n"
	if err := w.WriteSkill("test-skill", content); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	got, err := w.ReadSkill("test-skill")
	if err != nil {
		t.Fatalf("ReadSkill failed: %v", err)
	}

	if got != content {
		t.Errorf("ReadSkill content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

// TestWriterArchiveSkill verifies that ArchiveSkill moves the skill to the
// archive directory.
func TestWriterArchiveSkill(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: test-skill\ndescription: A test skill\n---\n\nBody.\n"
	if err := w.WriteSkill("test-skill", content); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	// Verify it exists in the skills dir.
	skillPath := filepath.Join(dir, "test-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("Skill file should exist before archive: %v", err)
	}

	// Archive it.
	if err := w.ArchiveSkill("test-skill"); err != nil {
		t.Fatalf("ArchiveSkill failed: %v", err)
	}

	// Verify it no longer exists in the skills dir.
	if _, err := os.Stat(skillPath); err == nil {
		t.Error("Skill file should NOT exist after archive")
	}

	// Verify it exists in the archive dir.
	archivePath := filepath.Join(dir+".archived", "test-skill", "SKILL.md")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("Archived file should exist: %v", err)
	}

	if string(data) != content {
		t.Errorf("Archived content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}
}

// TestWriterRestoreSkill verifies the full write -> archive -> restore cycle.
func TestWriterRestoreSkill(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: test-skill\ndescription: A test skill\n---\n\nRestore me.\n"
	if err := w.WriteSkill("test-skill", content); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	// Archive.
	if err := w.ArchiveSkill("test-skill"); err != nil {
		t.Fatalf("ArchiveSkill failed: %v", err)
	}

	// Restore.
	if err := w.RestoreSkill("test-skill"); err != nil {
		t.Fatalf("RestoreSkill failed: %v", err)
	}

	// Verify it's back in the skills dir.
	skillPath := filepath.Join(dir, "test-skill", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("Restored file should exist: %v", err)
	}

	if string(data) != content {
		t.Errorf("Restored content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}

	// Verify it's gone from the archive dir.
	archivePath := filepath.Join(dir+".archived", "test-skill", "SKILL.md")
	if _, err := os.Stat(archivePath); err == nil {
		t.Error("Archive file should NOT exist after restore")
	}
}

// TestWriterArchiveNotExists verifies that archiving a non-existent skill
// returns an error.
func TestWriterArchiveNotExists(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	err := w.ArchiveSkill("nonexistent")
	if err == nil {
		t.Error("ArchiveSkill should fail for non-existent skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' in error, got: %v", err)
	}
}

// TestWriterReadNotExists verifies that reading a non-existent skill returns
// an error.
func TestWriterReadNotExists(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	_, err := w.ReadSkill("nonexistent")
	if err == nil {
		t.Error("ReadSkill should fail for non-existent skill")
	}
}

// TestWriterRegistrySync verifies that ArchiveSkill and RestoreSkill keep the
// registry in sync with disk state.
func TestWriterRegistrySync(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	// Create a registry and register the skill.
	r := skills.NewRegistry()
	w.SetRegistry(r)

	content := "---\nname: test-skill\ndescription: A test skill\n---\n\nRegistry sync.\n"
	if err := w.WriteSkill("test-skill", content); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	// Manually register (WriteSkill does not auto-register).
	skill, err := skills.ParseSkillFile(filepath.Join(dir, "test-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("ParseSkillFile failed: %v", err)
	}
	r.Register(skill)

	// Verify registered.
	if r.Get("test-skill") == nil {
		t.Fatal("Skill should be registered before archive")
	}

	// Archive — should unregister.
	if err := w.ArchiveSkill("test-skill"); err != nil {
		t.Fatalf("ArchiveSkill failed: %v", err)
	}

	if r.Get("test-skill") != nil {
		t.Error("Skill should be unregistered after archive")
	}

	// Restore — should re-register.
	if err := w.RestoreSkill("test-skill"); err != nil {
		t.Fatalf("RestoreSkill failed: %v", err)
	}

	if r.Get("test-skill") == nil {
		t.Error("Skill should be registered after restore")
	}
}

// TestWriterAtomicWrite verifies that no .tmp file is left behind after a
// successful write.
func TestWriterAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	if err := w.WriteSkill("test-skill", "content"); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	// Check no .tmp file exists.
	tmpPath := filepath.Join(dir, "test-skill", "SKILL.md.tmp")
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("Temp file should not exist after successful write")
	}
}

// TestWriterArchiveSnapshots verifies that ArchiveSkill captures a version
// snapshot before moving the skill to the archive directory, when a Versioner
// is wired. The snapshot's version bundle moves with the skill dir to the
// archive location (the move relocates the entire skill dir including
// versions/).
func TestWriterArchiveSnapshots(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)
	v := NewVersioner(dir, nil)
	w.SetVersioner(v)

	content := "---\nname: skill-arch\n---\n\narchive snapshot body\n"
	if err := w.WriteSkill("skill-arch", content); err != nil {
		t.Fatalf("WriteSkill failed: %v", err)
	}

	// Archive — should snapshot v1 before moving.
	if err := w.ArchiveSkill("skill-arch"); err != nil {
		t.Fatalf("ArchiveSkill failed: %v", err)
	}

	// The version bundle should exist inside the archive dir (the snapshot
	// was created at the primary path, then the entire dir was moved).
	archivedBundle := filepath.Join(dir+".archived", "skill-arch", "versions", "v1", "bundle.json")
	if _, err := os.Stat(archivedBundle); err != nil {
		t.Fatalf("version bundle should exist in archive after ArchiveSkill: %v", err)
	}

	// Parse and verify the bundle manifest.
	bundleBytes, err := os.ReadFile(archivedBundle)
	if err != nil {
		t.Fatalf("reading bundle.json: %v", err)
	}
	var entry VersionEntry
	if err := parseJSONForTest(bundleBytes, &entry); err != nil {
		t.Fatalf("parsing bundle.json: %v", err)
	}
	if entry.Version != 1 {
		t.Errorf("snapshot version = %d, want 1", entry.Version)
	}
	if entry.Action != "snapshot" {
		t.Errorf("snapshot action = %q, want %q", entry.Action, "snapshot")
	}

	// The versioned SKILL.md should match the original content.
	archivedSkill := filepath.Join(dir+".archived", "skill-arch", "versions", "v1", "SKILL.md")
	data, err := os.ReadFile(archivedSkill)
	if err != nil {
		t.Fatalf("reading versioned SKILL.md: %v", err)
	}
	if string(data) != content {
		t.Errorf("versioned content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}
}

// TestWriterRestoreSnapshots verifies that RestoreSkill captures a version
// snapshot of the existing primary-path content before overwriting it with the
// archived copy, when a Versioner is wired.
//
// To ensure the snapshot survives the restore (which replaces the skill dir),
// the archive is created WITHOUT a versioner (so no versions/ in the archive
// dir). When RestoreSkill runs, it snapshots the current primary content into
// versions/v1/, then copyDir merges the archived SKILL.md over the primary —
// the versions/ dir survives because the archive has no versions/ to overwrite
// it with.
func TestWriterRestoreSnapshots(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	// 1. Write initial skill WITHOUT versioner and archive it. The archive
	//    dir will have NO versions/ subdir.
	initial := "---\nname: skill-rs\n---\n\ninitial\n"
	if err := w.WriteSkill("skill-rs", initial); err != nil {
		t.Fatalf("WriteSkill initial: %v", err)
	}
	if err := w.ArchiveSkill("skill-rs"); err != nil {
		t.Fatalf("ArchiveSkill: %v", err)
	}

	// 2. Write a DIFFERENT skill at the primary path (no versioner yet).
	current := "---\nname: skill-rs\n---\n\ncurrent at primary\n"
	if err := w.WriteSkill("skill-rs", current); err != nil {
		t.Fatalf("WriteSkill current: %v", err)
	}

	// 3. Wire the versioner NOW — only RestoreSkill will use it.
	v := NewVersioner(dir, nil)
	w.SetVersioner(v)

	// 4. Restore from archive — should snapshot the current primary content
	//    before overwriting.
	if err := w.RestoreSkill("skill-rs"); err != nil {
		t.Fatalf("RestoreSkill failed: %v", err)
	}

	// The snapshot of the "current at primary" content should exist (the
	// archive had no versions/ dir, so copyDir left it intact).
	versionedPath := filepath.Join(dir, "skill-rs", "versions", "v1", "SKILL.md")
	data, err := os.ReadFile(versionedPath)
	if err != nil {
		t.Fatalf("reading versioned skill: %v", err)
	}
	if string(data) != current {
		t.Errorf("snapshot content should be the overwritten primary content:\ngot:  %q\nwant: %q", string(data), current)
	}

	// Verify the live skill now has the restored (initial) content.
	live, err := w.ReadSkill("skill-rs")
	if err != nil {
		t.Fatalf("ReadSkill after restore: %v", err)
	}
	if live != initial {
		t.Errorf("live content after restore should be initial:\ngot:  %q\nwant: %q", live, initial)
	}
}

// TestWriterArchivePrunesSHAIndex verifies that archiving a skill removes its
// entries from the SHA index, so that writing a new skill with identical
// content does NOT trigger a false-positive duplicate detection.
func TestWriterArchivePrunesSHAIndex(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	content := "---\nname: skill-original\n---\n\nprune sha index\n"
	if err := w.WriteSkill("skill-original", content); err != nil {
		t.Fatalf("WriteSkill skill-original: %v", err)
	}

	// Verify dedup is active: writing same content under new name should fail.
	dupErr := w.WriteSkill("skill-other", content)
	if dupErr == nil {
		t.Fatal("WriteSkill skill-other with identical content should be rejected before archive")
	}

	// Archive the original — should prune its SHA index entry.
	if err := w.ArchiveSkill("skill-original"); err != nil {
		t.Fatalf("ArchiveSkill: %v", err)
	}

	// Now writing the same content under a new name should succeed (no dedup).
	if err := w.WriteSkill("skill-new", content); err != nil {
		t.Errorf("WriteSkill skill-new after archive of skill-original should succeed, got: %v", err)
	}

	// Verify skill-new was actually written.
	if _, err := os.Stat(filepath.Join(dir, "skill-new", "SKILL.md")); err != nil {
		t.Errorf("skill-new should exist after post-archive write: %v", err)
	}
}
