package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/eval"
)

// newJudgmentTestHandler builds an eval handler over an explicit dir so
// tests can assert on judgment files landing beside run records.
func newJudgmentTestHandler(t *testing.T, dir string) *EvalHandler {
	t.Helper()
	h := NewEvalHandler(dir, nil)
	h.runTimeout = 1 * time.Second
	return h
}

// postEvalRun drives handleRun with the given params JSON and decodes the
// created RunRecord.
func postEvalRun(t *testing.T, h *EvalHandler, params map[string]any) *eval.RunRecord {
	t.Helper()
	body, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleRun(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var run eval.RunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run record: %v", err)
	}
	return &run
}

func TestEvalRunAttachesJudgment(t *testing.T) {
	dir := t.TempDir()
	h := newJudgmentTestHandler(t, dir)

	run := postEvalRun(t, h, map[string]any{
		"task_id":  "tj",
		"model_id": "m",
		"k":        1,
		"command":  "true",
		"workdir":  t.TempDir(),
		"trajectory": []map[string]any{
			{"name": "read_file"},
			{"name": "edit_file"},
		},
	})

	store := eval.NewDiskStore(dir)
	j, err := store.LoadJudgment(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load judgment: %v", err)
	}
	if j.TrajectoryID != run.ID {
		t.Errorf("judgment trajectory_id = %q, want run id %q", j.TrajectoryID, run.ID)
	}
	if !j.Passed {
		t.Errorf("expected passed judgment, got %+v", j)
	}
	if j.FirstErrorStep != 0 {
		t.Errorf("expected FirstErrorStep=0, got %d", j.FirstErrorStep)
	}
	if j.OracleName == "" {
		t.Errorf("expected oracle name recorded, got empty")
	}
	// File lands beside the run record.
	if _, err := eval.NewDiskStore(dir).Get(context.Background(), run.ID); err != nil {
		t.Errorf("run record should exist beside judgment: %v", err)
	}
}

func TestEvalRunJudgmentFirstErrorStep(t *testing.T) {
	dir := t.TempDir()
	h := newJudgmentTestHandler(t, dir)

	// Passing oracle command, but the trajectory carries a failing first
	// step: judgment must record FirstErrorStep=1 and not pass.
	run := postEvalRun(t, h, map[string]any{
		"task_id":  "tj-fail",
		"model_id": "m",
		"k":        1,
		"command":  "true",
		"workdir":  t.TempDir(),
		"trajectory": []map[string]any{
			{"name": "go_test", "err": "exit status 1", "failed": true},
			{"name": "edit_file"},
		},
	})

	j, err := eval.NewDiskStore(dir).LoadJudgment(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load judgment: %v", err)
	}
	if j.Passed {
		t.Errorf("expected failed judgment, got %+v", j)
	}
	if j.FirstErrorStep != 1 {
		t.Errorf("expected FirstErrorStep=1, got %d", j.FirstErrorStep)
	}
}

func TestEvalRunNoTrajectoryNoJudgment(t *testing.T) {
	dir := t.TempDir()
	h := newJudgmentTestHandler(t, dir)

	run := postEvalRun(t, h, map[string]any{
		"task_id":  "tj-plain",
		"model_id": "m",
		"k":        1,
		"command":  "true",
		"workdir":  t.TempDir(),
	})

	if _, err := eval.NewDiskStore(dir).LoadJudgment(context.Background(), run.ID); !errors.Is(err, eval.ErrJudgmentNotFound) {
		t.Errorf("expected ErrJudgmentNotFound for untraj run, got %v", err)
	}
	// And no stray judgment files.
	matches, _ := filepath.Glob(filepath.Join(dir, "*.judgment.json"))
	if len(matches) != 0 {
		t.Errorf("expected no judgment files, got %v", matches)
	}
}

func TestEvalRunRPCAttachesJudgment(t *testing.T) {
	dir := t.TempDir()
	h := newJudgmentTestHandler(t, dir)

	params, err := json.Marshal(map[string]any{
		"task_id":  "tj-rpc",
		"model_id": "m",
		"k":        1,
		"command":  "true",
		"workdir":  t.TempDir(),
		"trajectory": []map[string]any{
			{"name": "read_file"},
			{"name": "go_test", "err": "FAIL", "failed": true},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	handlers := h.EvalRPCHandlers()
	out, err := handlers["eval.run"](context.Background(), params)
	if err != nil {
		t.Fatalf("eval.run: %v", err)
	}
	run, ok := out.(*eval.RunRecord)
	if !ok {
		t.Fatalf("expected *eval.RunRecord, got %T", out)
	}

	j, err := eval.NewDiskStore(dir).LoadJudgment(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load judgment: %v", err)
	}
	if j.Passed || j.FirstErrorStep != 2 {
		t.Errorf("expected failed judgment at step 2, got %+v", j)
	}
}
