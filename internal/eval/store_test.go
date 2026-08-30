package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestRecord(taskID string, createdAt time.Time) *RunRecord {
	rec := NewRun(KindPassK, taskID, "model-a", 2)
	rec.CreatedAt = createdAt
	return rec
}

func TestDiskStoreSaveGetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewDiskStore(dir)

	rec := NewRun(KindPassK, "task-1", "model-a", 3)
	rec.HarnessHash = "abc123"
	rec.AddAttempt(Attempt{Index: 0, ModelID: "model-a", Passed: true})
	rec.AddAttempt(Attempt{Index: 1, ModelID: "model-a", Passed: true})
	rec.Passed = PassK(rec.Attempts, rec.K)

	if err := store.Save(context.Background(), *rec); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Get(context.Background(), rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != rec.ID || got.TaskID != rec.TaskID || got.ModelID != rec.ModelID || got.K != rec.K {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", got, *rec)
	}
	if got.Passed != rec.Passed || got.HarnessHash != rec.HarnessHash {
		t.Fatalf("verdict/hash mismatch: got %+v", got)
	}
	if len(got.Attempts) != 2 || !got.Attempts[0].Passed || !got.Attempts[1].Passed {
		t.Fatalf("attempts mismatch: %+v", got.Attempts)
	}

	// The record lands at the documented path shape.
	if _, err := os.Stat(filepath.Join(dir, rec.ID+".json")); err != nil {
		t.Fatalf("record file missing: %v", err)
	}
}

func TestDiskStoreGetUnknownID(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	_, err := store.Get(context.Background(), "eval-does-not-exist")
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("want ErrRunNotFound, got %v", err)
	}
}

func TestDiskStoreGetRefusesTraversal(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	for _, id := range []string{"", ".", "..", "../escape", `a\b`, "x/../y"} {
		if _, err := store.Get(context.Background(), id); err == nil || errors.Is(err, ErrRunNotFound) {
			t.Fatalf("id %q: want invalid-id error, got %v", id, err)
		}
	}
}

func TestDiskStoreListNewestFirst(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		rec := newTestRecord(fmt.Sprintf("task-%d", i), base.Add(time.Duration(i)*time.Minute))
		if err := store.Save(context.Background(), *rec); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	runs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 5 {
		t.Fatalf("want 5 runs, got %d", len(runs))
	}
	for i, run := range runs {
		want := fmt.Sprintf("task-%d", 4-i)
		if run.TaskID != want {
			t.Fatalf("runs[%d].TaskID = %q, want %q", i, run.TaskID, want)
		}
	}
}

func TestDiskStoreListCapsAt50(t *testing.T) {
	store := NewDiskStore(t.TempDir())
	base := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < maxListRuns+5; i++ {
		rec := newTestRecord(fmt.Sprintf("task-%03d", i), base.Add(time.Duration(i)*time.Minute))
		if err := store.Save(context.Background(), *rec); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	runs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != maxListRuns {
		t.Fatalf("want %d runs, got %d", maxListRuns, len(runs))
	}
	// Newest first: the oldest five must have been dropped.
	if runs[0].TaskID != fmt.Sprintf("task-%03d", maxListRuns+4) {
		t.Fatalf("newest = %q", runs[0].TaskID)
	}
	if runs[maxListRuns-1].TaskID != "task-005" {
		t.Fatalf("oldest kept = %q, want task-005", runs[maxListRuns-1].TaskID)
	}
}

func TestDiskStoreListMissingDirIsEmpty(t *testing.T) {
	store := NewDiskStore(filepath.Join(t.TempDir(), "nope"))
	runs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list on missing dir: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("want empty list, got %d", len(runs))
	}
}
