package llm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newRetentionTestRoot builds a spill tree under a temp meept home and returns
// the home plus a helper to create a file with a backdated mtime.
func newRetentionTestRoot(t *testing.T) (string, func(rel string, age time.Duration, content string) string) {
	t.Helper()
	home := t.TempDir()
	write := func(rel string, age time.Duration, content string) string {
		t.Helper()
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		mtime := time.Now().Add(-age)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
		return path
	}
	return home, write
}

func TestRetentionSweepDeletesOldFiles(t *testing.T) {
	home, write := newRetentionTestRoot(t)
	oldA := write(filepath.Join("spill", "sess-a", "old.txt"), 40*24*time.Hour, "old a")
	oldB := write(filepath.Join("spill", "sess-b", "old.txt"), 40*24*time.Hour, "old b")
	fresh := write(filepath.Join("spill", "sess-a", "new.txt"), 1*24*time.Hour, "fresh")
	write(filepath.Join("spill", "sess-a", "sub", "old.txt"), 40*24*time.Hour, "old nested")

	removed, err := SweepSpill(context.Background(), SpillRetentionOptions{
		RetentionDays: 30,
		MeeptHome:     home,
	})
	if err != nil {
		t.Fatalf("SweepSpill: %v", err)
	}
	if removed != 3 {
		t.Errorf("removed = %d, want 3", removed)
	}
	if _, err := os.Stat(oldA); !os.IsNotExist(err) {
		t.Errorf("old file A still exists (err=%v)", err)
	}
	if _, err := os.Stat(oldB); !os.IsNotExist(err) {
		t.Errorf("old file B still exists (err=%v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file removed: %v", err)
	}
	// sess-b had only the old file: the dir (and its empty parents) must go.
	if _, err := os.Stat(filepath.Join(home, "spill", "sess-b")); !os.IsNotExist(err) {
		t.Errorf("emptied session dir sess-b still exists (err=%v)", err)
	}
	// sess-a still holds the fresh file: dir must remain.
	if _, err := os.Stat(filepath.Join(home, "spill", "sess-a")); err != nil {
		t.Errorf("active session dir sess-a removed: %v", err)
	}
	// The spill root itself is never removed.
	if _, err := os.Stat(filepath.Join(home, "spill")); err != nil {
		t.Errorf("spill root removed: %v", err)
	}
}

func TestRetentionDisabledKeepsAll(t *testing.T) {
	home, write := newRetentionTestRoot(t)
	paths := []string{
		write(filepath.Join("spill", "sess-a", "old.txt"), 400*24*time.Hour, "old"),
		write(filepath.Join("spill", "sess-b", "older.txt"), 4000*24*time.Hour, "older"),
	}
	for _, days := range []int{0, -1} {
		removed, err := SweepSpill(context.Background(), SpillRetentionOptions{
			RetentionDays: days,
			MeeptHome:     home,
		})
		if err != nil {
			t.Fatalf("SweepSpill(retention=%d): %v", days, err)
		}
		if removed != 0 {
			t.Errorf("retention=%d: removed %d files, want 0", days, removed)
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("retention=%d: file %s removed: %v", days, p, err)
			}
		}
	}
}

func TestRetentionLoopStopsOnContextCancel(t *testing.T) {
	home, _ := newRetentionTestRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		SpillRetentionLoop(ctx, SpillRetentionOptions{RetentionDays: 0, MeeptHome: home})
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// loop returned promptly: ticker goroutine does not leak.
	case <-time.After(2 * time.Second):
		t.Fatal("SpillRetentionLoop did not return after context cancel")
	}
}

func TestRetentionNeverEscapesSpillRoot(t *testing.T) {
	home, write := newRetentionTestRoot(t)
	decoys := []string{
		write(filepath.Join("decoy", "old.txt"), 40*24*time.Hour, "decoy file"),
		write(filepath.Join("spill-sibling", "old.txt"), 40*24*time.Hour, "sibling dir that merely shares a prefix"),
	}
	removed, err := SweepSpill(context.Background(), SpillRetentionOptions{
		RetentionDays: 30,
		MeeptHome:     home,
	})
	if err != nil {
		t.Fatalf("SweepSpill: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed %d files, want 0 (sweep escaped the spill root)", removed)
	}
	for _, p := range decoys {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("decoy file %s touched: %v", p, err)
		}
	}
}
