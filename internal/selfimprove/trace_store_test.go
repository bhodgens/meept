package selfimprove

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkgid "github.com/caimlas/meept/pkg/id"
)

// fixedNoon is the fixed-noon-UTC time fixture convention for tests that
// depend on the dated-directory layout (traces/<yyyy-mm-dd>/).
var fixedNoon = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

func TestTraceStoreWrite_CreatesDatedFile(t *testing.T) {
	ts := NewTraceStore(t.TempDir(), slogDiscardLogger())
	rec := &TraceRecord{
		ID:        pkgid.Generate("trace-"),
		SessionID: "conv-1",
		Outcome:   TraceOutcomeFailure,
		Error:     "boom",
		Steps:     []TraceStep{{Action: "assistant_response", Output: "x", Success: true}},
		CreatedAt: fixedNoon,
	}
	path, err := ts.Write(rec)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(path, filepath.Join("traces", "2025-06-15")) {
		t.Fatalf("path missing dated dir: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got TraceRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != rec.ID || got.Outcome != TraceOutcomeFailure {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Error != "boom" {
		t.Fatalf("error field not persisted: %q", got.Error)
	}
	if got.CreatedAt != fixedNoon {
		t.Fatalf("created_at round-trip mismatch: %v", got.CreatedAt)
	}
	// .tmp leftovers must not exist (atomic rename pattern).
	entries, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
	if len(entries) != 0 {
		t.Fatalf("stray tmp files: %v", entries)
	}
}

func TestTraceStoreWrite_JSONShape(t *testing.T) {
	ts := NewTraceStore(t.TempDir(), slogDiscardLogger())
	rec := &TraceRecord{
		ID:             "trace-deadbeefdeadbeef",
		SessionID:      "conv-2",
		Domain:         "code",
		Outcome:        TraceOutcomeSuccess,
		InjectedSkills: []string{"reviewer", "tester"},
		Steps: []TraceStep{
			{Action: "user_input", Input: "q", Success: true},
			{Action: "assistant_response", Output: "a", Success: true},
		},
		Summary:   "did the thing",
		CreatedAt: fixedNoon,
	}
	path, err := ts.Write(rec)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// omitempty fields absent on success records.
	for _, key := range []string{"error"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("empty field %q must be omitted on success record", key)
		}
	}
	for _, key := range []string{"id", "session_id", "domain", "outcome", "injected_skills", "steps", "summary", "created_at"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("field %q missing from JSON", key)
		}
	}
	steps, ok := raw["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("steps not serialized as array of 2: %v", raw["steps"])
	}
	first, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("step 0 not an object: %v", steps[0])
	}
	if first["action"] != "user_input" {
		t.Fatalf("step action tag mismatch: %v", first)
	}
	if _, hasInput := first["input"]; !hasInput {
		t.Fatalf("step input tag missing: %v", first)
	}
	if _, hasOutput := first["output"]; hasOutput {
		t.Fatalf("empty step output must be omitted: %v", first)
	}
}

func TestTraceStoreWrite_RejectsEmptyID(t *testing.T) {
	ts := NewTraceStore(t.TempDir(), slogDiscardLogger())
	if _, err := ts.Write(&TraceRecord{SessionID: "conv-3", CreatedAt: fixedNoon}); err == nil {
		t.Fatal("expected error for empty record ID")
	}
}

func TestTraceStoreWrite_NilLoggerWorks(t *testing.T) {
	ts := NewTraceStore(t.TempDir(), nil)
	if _, err := ts.Write(&TraceRecord{
		ID:        pkgid.Generate("trace-"),
		SessionID: "conv-4",
		Outcome:   TraceOutcomeSuccess,
		CreatedAt: fixedNoon,
	}); err != nil {
		t.Fatalf("write with nil logger: %v", err)
	}
}

func TestTraceStoreSample_Stratified(t *testing.T) {
	dir := t.TempDir()
	ts := NewTraceStore(dir, slogDiscardLogger())
	base := fixedNoon
	for i := 0; i < 4; i++ {
		_, err := ts.Write(&TraceRecord{
			ID:        fmt.Sprintf("f%d", i),
			Outcome:   TraceOutcomeFailure,
			Error:     fmt.Sprintf("err-%d", i),
			CreatedAt: base,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		_, err := ts.Write(&TraceRecord{
			ID:        fmt.Sprintf("s%d", i),
			Outcome:   TraceOutcomeSuccess,
			CreatedAt: base,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := ts.Sample(2, 1, 100000)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	fails, passes := 0, 0
	for _, r := range got {
		if r.Outcome == TraceOutcomeFailure {
			fails++
		} else {
			passes++
		}
	}
	if fails != 2 || passes != 1 {
		t.Fatalf("stratification wrong: %d fails, %d passes", fails, passes)
	}
}

func TestTraceStoreSample_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	ts := NewTraceStore(dir, slogDiscardLogger())
	// Two date dirs: older 2025-06-14, newer 2025-06-15. IDs sort lexically
	// within a dir (see Sample doc comment).
	dates := []time.Time{
		time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC),
		fixedNoon,
	}
	wantID := ""
	for di, at := range dates {
		for i := 0; i < 3; i++ {
			id := fmt.Sprintf("d%d-%02d", di, i)
			if _, err := ts.Write(&TraceRecord{
				ID:        id,
				Outcome:   TraceOutcomeFailure,
				Error:     id,
				CreatedAt: at,
			}); err != nil {
				t.Fatal(err)
			}
			if di == 1 && i == 2 {
				wantID = id // lexically last in the newest dir
			}
		}
	}
	got, err := ts.Sample(1, 0, 100000)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].ID != wantID {
		t.Fatalf("newest-first walk wrong: want %q, got %q", wantID, got[0].ID)
	}
}

func TestTraceStoreSample_SkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	ts := NewTraceStore(dir, slogDiscardLogger())
	for i := 0; i < 3; i++ {
		if _, err := ts.Write(&TraceRecord{
			ID:        fmt.Sprintf("ok%d", i),
			Outcome:   TraceOutcomeFailure,
			CreatedAt: fixedNoon,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Corrupt one file on disk.
	corrupt := filepath.Join(dir, "traces", "2025-06-15", "ok1.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Sample(5, 5, 100000)
	if err != nil {
		t.Fatalf("sample must not fail on corrupt file: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 surviving records, got %d", len(got))
	}
	for _, r := range got {
		if r.ID == "ok1" {
			t.Fatal("corrupt record must be skipped, not returned")
		}
	}
}

func TestTraceStoreSample_TruncatesLongSteps(t *testing.T) {
	dir := t.TempDir()
	ts := NewTraceStore(dir, slogDiscardLogger())
	long := strings.Repeat("a", 2000)
	if _, err := ts.Write(&TraceRecord{
		ID:        "big",
		Outcome:   TraceOutcomeFailure,
		Steps:     []TraceStep{{Action: "assistant_response", Output: long, Success: true}},
		CreatedAt: fixedNoon,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Sample(1, 0, 100)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	rec := got[0]
	if len(rec.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(rec.Steps))
	}
	out := rec.Steps[0].Output
	if !strings.Contains(out, "...[truncated]") {
		t.Fatalf("truncation marker missing: %q", out[:min(80, len(out))])
	}
	// Capped total step text must respect maxChars (marker included).
	total := len(rec.Steps[0].Input) + len(out)
	if total > 100 {
		t.Fatalf("capped step text %d exceeds maxChars 100", total)
	}
	if out == long {
		t.Fatal("output was not truncated")
	}
}

func TestTraceStoreSample_IgnoresNonTraceDirsAndFiles(t *testing.T) {
	dir := t.TempDir()
	ts := NewTraceStore(dir, slogDiscardLogger())
	// Stray files and dirs inside traces/ must not break the walk.
	strays := []string{
		filepath.Join(dir, "traces", "not-a-date", "junk.json"),
		filepath.Join(dir, "traces", "stray.txt"),
	}
	for _, p := range strays {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ts.Write(&TraceRecord{
		ID:        "real",
		Outcome:   TraceOutcomeSuccess,
		CreatedAt: fixedNoon,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Sample(0, 5, 100000)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 1 || got[0].ID != "real" {
		t.Fatalf("unexpected sample results: %+v", got)
	}
}

func TestTraceStoreSample_EmptyStore(t *testing.T) {
	ts := NewTraceStore(t.TempDir(), slogDiscardLogger())
	got, err := ts.Sample(3, 3, 1000)
	if err != nil {
		t.Fatalf("sample on empty store: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 records, got %d", len(got))
	}
}
