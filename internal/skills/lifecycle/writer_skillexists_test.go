package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriterSkillExists pins the existence probe the evolver prune pass uses
// as its stale-target guard (decision brief 2026-10-22 item 2). It must
// agree with ArchiveSkill's own resolution: true for a skill present on disk,
// false for one already gone and for an empty name.
func TestWriterSkillExists(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, nil)

	// Absent skill -> false.
	if w.SkillExists("missing-skill") {
		t.Fatal("SkillExists(missing) = true, want false")
	}

	// Empty name -> false.
	if w.SkillExists("") {
		t.Fatal("SkillExists(\"\") = true, want false")
	}

	// Write a skill -> now true.
	content := "---\nname: present-skill\ndescription: d\n---\n\n# present-skill\n"
	if err := w.WriteSkill("present-skill", content); err != nil {
		t.Fatalf("WriteSkill: %v", err)
	}
	if !w.SkillExists("present-skill") {
		t.Fatal("SkillExists(present) = false, want true")
	}

	// Remove the skill dir -> false again (this is the stale-stats case that
	// produced the 0/252 archive failures).
	if err := os.RemoveAll(filepath.Join(dir, "present-skill")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if w.SkillExists("present-skill") {
		t.Fatal("SkillExists after removal = true, want false")
	}
}

// TestWriterSkillExists_NilReceiver proves the nil guard (the evolver calls
// this on a possibly-nil writer).
func TestWriterSkillExists_NilReceiver(t *testing.T) {
	var w *Writer
	if w.SkillExists("anything") {
		t.Fatal("nil Writer.SkillExists = true, want false")
	}
}
