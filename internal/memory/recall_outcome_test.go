package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Leaf 04 T1: the outcome writer appends C5 rows, honors the shared 10 MiB
// calibration cap (one .1 generation, cap BEFORE append), never panics on
// malformed usage, and disables itself after a write failure.
func TestRecallOutcomeWriter(t *testing.T) {
	t.Run("appends C5 rows", func(t *testing.T) {
		dir := t.TempDir()
		logger := NewRecallOutcomeLogger(dir, nil)
		if logger == nil {
			t.Fatal("NewRecallOutcomeLogger returned nil for a valid data dir")
		}
		items := []RecallOutcomeRecord{{
			MemoryID: "mem-1",
			Query:    "what is the convoy passphrase",
			Tokens:   TokenEstimate("the convoy passphrase is amber Otter 4431"),
			Salient:  SalientTokens("the convoy passphrase is amber Otter 4431"),
		}}
		// A reply that restates the item: >= 8 shared tokens -> cited.
		logger.LogOutcomes(context.Background(), "sess-1", items,
			"the convoy passphrase is amber Otter 4431, confirmed for the operation.")
		// An unrelated reply: cited=false (conservative tie goes low).
		logger.LogOutcomes(context.Background(), "sess-1", items,
			"nothing to see here at all.")

		rows, err := OutcomeRows(logger.Path())
		if err != nil {
			t.Fatalf("OutcomeRows: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2", len(rows))
		}
		if !rows[0].Cited || !rows[0].Injected {
			t.Errorf("row 0 cited/injected = %v/%v, want true/true", rows[0].Cited, rows[0].Injected)
		}
		if rows[1].Cited {
			t.Errorf("row 1 cited = true, want false (unrelated reply)")
		}
		if rows[0].MemoryID != "mem-1" || rows[0].SessionID != "sess-1" || rows[0].Query == "" {
			t.Errorf("row 0 identity fields incomplete: %+v", rows[0])
		}
		if rows[0].Tokens <= 0 {
			t.Errorf("row 0 tokens = %d, want the item's estimate (>0)", rows[0].Tokens)
		}
		if _, err := time.Parse(time.RFC3339, rows[0].TS); err != nil {
			t.Errorf("row 0 ts %q not RFC3339: %v", rows[0].TS, err)
		}
	})

	t.Run("citation threshold boundary", func(t *testing.T) {
		item := SalientTokens("alpha beta gamma delta epsilon zeta eta theta")
		if len(item) != 8 {
			t.Fatalf("fixture tokens = %d, want 8", len(item))
		}
		exact := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
		if !CitedByOverlap(exact, item) {
			t.Error("8 shared tokens must cite (>= bar)")
		}
		seven := exact[:7]
		if CitedByOverlap(seven, item) {
			t.Error("7 shared tokens must NOT cite (conservative)")
		}
		// Multiplicity: a reply restating a 7-unique-token fact with one
		// repeated article reaches 8 shared occurrences and cites (the
		// committed F5 fixture's exact shape).
		fact := SalientTokens("the convoy passphrase is amber Otter 4431")
		if len(fact) != 7 {
			t.Fatalf("fact tokens = %d, want 7", len(fact))
		}
		restated := ReplyTokens("the convoy passphrase is amber Otter 4431 — noted for the operation.")
		if CitedByOverlap(restated[:7], fact) {
			t.Error("first 7 reply tokens alone must NOT cite")
		}
		if !CitedByOverlap(restated, fact) {
			t.Error("the full restating reply must cite (8 shared occurrences)")
		}
		if CitedByOverlap(nil, item) || CitedByOverlap(exact, nil) {
			t.Error("empty side must never cite")
		}
	})

	t.Run("rotation honors the 10 MiB cap before append", func(t *testing.T) {
		dir := t.TempDir()
		logger := NewRecallOutcomeLogger(dir, nil)
		// Seed the file just over the shared calibration cap with valid rows
		// (one big filler token block per row). The next append must rotate
		// the existing file to .1 and write the new row fresh.
		row := RecallOutcomeRecord{
			MemoryID: "mem-big",
			Query:    "q",
			Tokens:   1,
			Salient:  SalientTokens("filler"),
		}
		var sb strings.Builder
		for sb.Len() <= int(MaxCalibrationLogBytes) {
			b, err := json.Marshal(row)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			sb.Write(b)
			sb.WriteByte('\n')
		}
		if err := os.WriteFile(logger.Path(), []byte(sb.String()), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		logger.LogOutcomes(context.Background(), "sess-rot", []RecallOutcomeRecord{row}, "unrelated short reply")

		rotated := logger.Path() + ".1"
		fi, err := os.Stat(rotated)
		if err != nil {
			t.Fatalf("rotated generation missing: %v", err)
		}
		if fi.Size() <= MaxCalibrationLogBytes {
			t.Errorf("rotated size = %d, want > cap", fi.Size())
		}
		rows, err := OutcomeRows(logger.Path())
		if err != nil {
			t.Fatalf("read current generation: %v", err)
		}
		if len(rows) != 1 || rows[0].SessionID != "sess-rot" {
			t.Errorf("current generation rows = %+v, want the single new row", rows)
		}
	})

	t.Run("write failure disables the logger without panicking", func(t *testing.T) {
		dir := t.TempDir()
		logger := NewRecallOutcomeLogger(dir, nil)
		// A DIRECTORY at the log path makes every append fail.
		if err := os.MkdirAll(logger.Path(), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		logger.LogOutcomes(context.Background(), "sess-x", []RecallOutcomeRecord{{
			MemoryID: "m", Query: "q", Salient: SalientTokens("x"),
		}}, "some reply")
		if got := logger.Path(); got != "" {
			t.Errorf("logger still enabled after a write failure (path %q)", got)
		}
		// Disabled logger is a no-op, not a panic.
		logger.LogOutcomes(context.Background(), "sess-x", nil, "")
		logger.BeginTurn("sess-x", nil)
		_ = logger.TakeRows("sess-x")
	})

	t.Run("pending snapshot staging is race-safe and carry-over safe", func(t *testing.T) {
		dir := t.TempDir()
		logger := NewRecallOutcomeLogger(dir, nil)
		items := []RecallOutcomeRecord{{MemoryID: "m1", Query: "q", Salient: SalientTokens("alpha beta")}}
		logger.BeginTurn("conv-1", items)
		got := logger.TakeRows("conv-1")
		if len(got) != 1 || got[0].MemoryID != "m1" {
			t.Fatalf("TakeRows = %+v, want the staged item", got)
		}
		// Carry-over: taking twice still returns the snapshot (the next
		// assembly replaces it; an empty assembly clears it).
		if got2 := logger.TakeRows("conv-1"); len(got2) != 1 {
			t.Errorf("second TakeRows = %+v, want carry-over", got2)
		}
		logger.BeginTurn("conv-1", nil)
		if got3 := logger.TakeRows("conv-1"); len(got3) != 0 {
			t.Errorf("TakeRows after explicit clear = %+v, want none", got3)
		}
		// Nil logger is fully nil-safe.
		var disabled *RecallOutcomeLogger
		disabled.BeginTurn("c", items)
		_ = disabled.TakeRows("c")
		disabled.LogOutcomes(context.Background(), "s", items, "r")
	})

	t.Run("empty data dir disables", func(t *testing.T) {
		if NewRecallOutcomeLogger("", nil) != nil {
			t.Error("empty dataDir must return nil")
		}
	})
}

// TestRecallOutcomeFilename pins the C5 filename under the data dir.
func TestRecallOutcomeFilename(t *testing.T) {
	dir := t.TempDir()
	logger := NewRecallOutcomeLogger(dir, nil)
	want := filepath.Join(dir, "recall_outcomes.jsonl")
	if logger.Path() != want {
		t.Errorf("path = %q, want %q", logger.Path(), want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("logger must not create the file until the first row: err=%v", err)
	}
	if fmt.Sprint(recallOutcomeFile) != "recall_outcomes.jsonl" {
		t.Error("filename contract drifted")
	}
}
