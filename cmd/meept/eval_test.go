package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/eval"
)

// runEvalRunFor is a helper that runs runEvalRun with a temp store and
// returns the combined output and the parsed printed run id. A completed but
// failed run (errEvalNotPassed) is not treated as fatal.
func runEvalRunFor(t *testing.T, storeDir string, opts EvalRunOptions) (string, string) {
	t.Helper()
	opts.StoreDir = storeDir
	var buf bytes.Buffer
	err := runEvalRun(context.Background(), &buf, opts)
	if err != nil && !errors.Is(err, errEvalNotPassed) {
		t.Fatalf("runEvalRun: %v", err)
	}
	out := buf.String()
	idLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "id: ") {
			idLine = strings.TrimPrefix(line, "id: ")
			break
		}
	}
	if idLine == "" {
		t.Fatalf("no id line in output:\n%s", out)
	}
	return out, idLine
}

func TestRunEvalRun_Passes(t *testing.T) {
	storeDir := t.TempDir()

	for _, k := range []int{1, 2, 3} {
		out, id := runEvalRunFor(t, storeDir, EvalRunOptions{
			TaskID:  "task-1",
			ModelID: "model-a",
			K:       k,
			Command: "true",
			Workdir: t.TempDir(),
		})
		if !strings.Contains(out, "passed: true") {
			t.Errorf("k=%d: expected 'passed: true' in output:\n%s", k, out)
		}
		if !strings.HasPrefix(id, "eval-") {
			t.Errorf("k=%d: unexpected id %q", k, id)
		}

		rec, err := (evalStore{dir: storeDir}).get(id)
		if err != nil {
			t.Fatalf("k=%d: load record: %v", k, err)
		}
		if rec.Kind != eval.KindPassK {
			t.Errorf("k=%d: kind = %q, want pass_k", k, rec.Kind)
		}
		if len(rec.Attempts) != k {
			t.Errorf("k=%d: got %d attempts, want %d", k, len(rec.Attempts), k)
		}
		if !rec.Passed {
			t.Errorf("k=%d: record not marked passed", k)
		}
		if rec.TaskID != "task-1" || rec.ModelID != "model-a" {
			t.Errorf("k=%d: task/model mismatch: %+v", k, rec)
		}
		if rec.HarnessHash == "" {
			t.Errorf("k=%d: empty harness hash", k)
		}
		if rec.HarnessHash != eval.HarnessHash("cli", "", "") {
			t.Errorf("k=%d: harness hash does not match eval.HarnessHash(\"cli\", \"\", \"\")", k)
		}
	}
}

func TestRunEvalRun_Fails(t *testing.T) {
	storeDir := t.TempDir()

	out, id := runEvalRunFor(t, storeDir, EvalRunOptions{
		TaskID:  "task-2",
		ModelID: "model-b",
		K:       2,
		Command: "false",
		Workdir: t.TempDir(),
	})
	if !strings.Contains(out, "passed: false") {
		t.Errorf("expected 'passed: false' in output:\n%s", out)
	}

	rec, err := (evalStore{dir: storeDir}).get(id)
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if rec.Passed {
		t.Error("record should not be passed")
	}
	if len(rec.Attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(rec.Attempts))
	}
	for _, a := range rec.Attempts {
		if a.Passed {
			t.Errorf("attempt %d should not have passed", a.Index)
		}
	}
}

func TestRunEvalRun_MissingWorkdir(t *testing.T) {
	var buf bytes.Buffer
	err := runEvalRun(context.Background(), &buf, EvalRunOptions{
		K:        1,
		Command:  "true",
		Workdir:  "",
		StoreDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for missing workdir")
	}
	if !strings.Contains(err.Error(), "workdir") {
		t.Errorf("error should mention workdir, got: %v", err)
	}
}

func TestRunEvalRun_BadWorkdirFailsClosed(t *testing.T) {
	storeDir := t.TempDir()

	out, id := runEvalRunFor(t, storeDir, EvalRunOptions{
		TaskID:  "task-3",
		ModelID: "model-c",
		K:       1,
		Command: "true",
		Workdir: filepath.Join(storeDir, "does-not-exist"),
	})
	if !strings.Contains(out, "passed: false") {
		t.Errorf("expected fail-closed 'passed: false' in output:\n%s", out)
	}

	rec, err := (evalStore{dir: storeDir}).get(id)
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if rec.Passed {
		t.Error("oracle run in missing workdir should fail closed")
	}
}

func TestRunEvalShow(t *testing.T) {
	storeDir := t.TempDir()

	_, id := runEvalRunFor(t, storeDir, EvalRunOptions{
		TaskID:  "task-show",
		ModelID: "model-d",
		K:       1,
		Command: "true",
		Workdir: t.TempDir(),
	})

	var buf bytes.Buffer
	if err := runEvalShow(context.Background(), &buf, storeDir, id); err != nil {
		t.Fatalf("runEvalShow: %v", err)
	}

	var rec eval.RunRecord
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("show output is not valid json: %v\n%s", err, buf.String())
	}
	if rec.ID != id || rec.TaskID != "task-show" {
		t.Errorf("showed wrong record: id=%q task=%q", rec.ID, rec.TaskID)
	}
	if !strings.Contains(buf.String(), `"task_id": "task-show"`) {
		t.Errorf("expected snake_case json field in output:\n%s", buf.String())
	}
}

func TestRunEvalShow_UnknownID(t *testing.T) {
	var buf bytes.Buffer
	err := runEvalShow(context.Background(), &buf, t.TempDir(), "eval-nonexistent0000")
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if err.Error() != "eval run not found: eval-nonexistent0000" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunEvalList_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := runEvalList(context.Background(), &buf, t.TempDir()); err != nil {
		t.Fatalf("runEvalList on empty store: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got:\n%s", buf.String())
	}
}

// saveRecordWithTime writes a record directly so ordering tests control
// CreatedAt exactly.
func saveRecordWithTime(t *testing.T, storeDir string, id string, createdAt time.Time) {
	t.Helper()
	rec := eval.NewRun(eval.KindPassK, "task-"+id, "model-x", 1)
	rec.ID = id
	rec.CreatedAt = createdAt
	rec.Passed = true
	if err := (evalStore{dir: storeDir}).save(rec); err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
}

func TestRunEvalList_NewestFirst(t *testing.T) {
	storeDir := t.TempDir()
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	saveRecordWithTime(t, storeDir, "eval-old000000000001", base)
	saveRecordWithTime(t, storeDir, "eval-new000000000001", base.Add(time.Hour))
	saveRecordWithTime(t, storeDir, "eval-mid000000000001", base.Add(30*time.Minute))

	var buf bytes.Buffer
	if err := runEvalList(context.Background(), &buf, storeDir); err != nil {
		t.Fatalf("runEvalList: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), buf.String())
	}
	wantOrder := []string{"eval-new000000000001", "eval-mid000000000001", "eval-old000000000001"}
	for i, want := range wantOrder {
		if !strings.HasPrefix(lines[i], want+"  ") {
			t.Errorf("line %d: got %q, want prefix %q", i, lines[i], want)
		}
	}

	// Format: <id>  <created_at RFC3339>  <task_id>  passed=true|false k=<k>
	if !strings.Contains(lines[0], " passed=true k=1") {
		t.Errorf("line 0 missing passed/k fields: %q", lines[0])
	}
	if !strings.Contains(lines[0], base.Add(time.Hour).Format(time.RFC3339)) {
		t.Errorf("line 0 missing RFC3339 timestamp: %q", lines[0])
	}
}

func TestRunEvalList_Cap50(t *testing.T) {
	storeDir := t.TempDir()
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	const total = evalListLimit + 5
	for i := 0; i < total; i++ {
		id := "eval-x" + pad16(i)
		saveRecordWithTime(t, storeDir, id, base.Add(time.Duration(i)*time.Minute))
	}

	var buf bytes.Buffer
	if err := runEvalList(context.Background(), &buf, storeDir); err != nil {
		t.Fatalf("runEvalList: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != evalListLimit {
		t.Fatalf("expected %d lines (capped), got %d", evalListLimit, len(lines))
	}
	// Newest first means the LAST saved (i = total-1) comes first.
	first := "eval-x" + pad16(total-1)
	if !strings.HasPrefix(lines[0], first+"  ") {
		t.Errorf("first line %q, want prefix %q", lines[0], first)
	}
	// Oldest shown is the (total - limit)-th.
	last := "eval-x" + pad16(total-evalListLimit)
	if !strings.HasPrefix(lines[evalListLimit-1], last+"  ") {
		t.Errorf("last line %q, want prefix %q", lines[evalListLimit-1], last)
	}
}

// pad16 renders n as 16 lowercase-hex-like chars (test ids only).
func pad16(n int) string {
	const digits = "0123456789abcdef"
	out := []byte{}
	for n > 0 || len(out) < 16 {
		out = append([]byte{digits[n%16]}, out...)
		n /= 16
	}
	return string(out)
}
