package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/eval"
)

// newTestEvalHandler returns a handler writing to a temp dir with a 1s
// per-attempt timeout so tests stay fast.
func newTestEvalHandler(t *testing.T) *EvalHandler {
	t.Helper()
	h := NewEvalHandler(t.TempDir(), nil)
	h.runTimeout = 1 * time.Second
	return h
}

// newEvalTestMux registers an EvalHandler's routes on a fresh mux.
func newEvalTestMux(t *testing.T) (*http.ServeMux, *EvalHandler) {
	t.Helper()
	h := newTestEvalHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, h
}

func TestEvalRunCreatesRecord(t *testing.T) {
	mux, h := newEvalTestMux(t)
	workdir := t.TempDir()

	body := `{"task_id":"task-1","model_id":"model-a","k":2,"command":"true","workdir":"` + workdir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval/runs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	var got eval.RunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// k=2 "true" oracle: both attempts ran and passed.
	if got.K != 2 || !got.Passed || len(got.Attempts) != 2 {
		t.Fatalf("unexpected record: k=%d passed=%v attempts=%d", got.K, got.Passed, len(got.Attempts))
	}
	if got.TaskID != "task-1" || got.ModelID != "model-a" {
		t.Fatalf("task/model mismatch: %+v", got)
	}
	if got.HarnessHash == "" || got.OracleName != "shell:true" {
		t.Fatalf("hash/oracle_name mismatch: %+v", got)
	}

	// The record was persisted and is fetchable.
	stored, err := h.store.Get(t.Context(), got.ID)
	if err != nil {
		t.Fatalf("stored record unreadable: %v", err)
	}
	if stored.ID != got.ID {
		t.Fatalf("stored id %q != response id %q", stored.ID, got.ID)
	}
}

func TestEvalRunRunsAllKAttempts(t *testing.T) {
	mux, _ := newEvalTestMux(t)
	workdir := t.TempDir()

	// Failing oracle: the contract runs all K attempts even on failure.
	body := `{"task_id":"t","model_id":"m","k":5,"command":"false","workdir":"` + workdir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval/runs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var got eval.RunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Attempts) != 5 {
		t.Fatalf("want 5 attempts (contract: run K times), got %d", len(got.Attempts))
	}
	if got.Passed {
		t.Fatal("run must not pass when the oracle fails")
	}
}

func TestEvalRunValidation(t *testing.T) {
	mux, _ := newEvalTestMux(t)
	workdir := t.TempDir()

	cases := []struct {
		name string
		body string
	}{
		{"k zero", `{"task_id":"t","model_id":"m","k":0,"command":"true","workdir":"` + workdir + `"}`},
		{"k negative", `{"task_id":"t","model_id":"m","k":-3,"command":"true","workdir":"` + workdir + `"}`},
		{"missing command", `{"task_id":"t","model_id":"m","k":1,"workdir":"` + workdir + `"}`},
		{"empty command", `{"task_id":"t","model_id":"m","k":1,"command":"","workdir":"` + workdir + `"}`},
		{"missing workdir", `{"task_id":"t","model_id":"m","k":1,"command":"true"}`},
		{"empty workdir", `{"task_id":"t","model_id":"m","k":1,"command":"true","workdir":""}`},
		{"malformed json", `{not json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/eval/runs", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			var errResp struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if errResp.Error == "" {
				t.Fatal("error message is empty")
			}
			if errResp.Error != strings.ToLower(errResp.Error[:1])+errResp.Error[1:] {
				t.Fatalf("error message not lowercase-first: %q", errResp.Error)
			}
		})
	}
}

func TestEvalListEmpty(t *testing.T) {
	mux, _ := newEvalTestMux(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval/runs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Runs []eval.RunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Runs == nil || len(got.Runs) != 0 {
		t.Fatalf("want non-nil empty runs, got %+v", got.Runs)
	}
}

func TestEvalListReturnsRecords(t *testing.T) {
	mux, h := newEvalTestMux(t)

	// Seed two records directly.
	for i := 0; i < 2; i++ {
		rec := eval.NewRun(eval.KindPassK, fmt.Sprintf("task-%d", i), "m", 1)
		rec.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Minute)
		if err := h.store.Save(t.Context(), *rec); err != nil {
			t.Fatalf("seed save: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/eval/runs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got struct {
		Runs []eval.RunRecord `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(got.Runs))
	}
	// Newest first.
	if got.Runs[0].TaskID != "task-1" || got.Runs[1].TaskID != "task-0" {
		t.Fatalf("ordering wrong: %q then %q", got.Runs[0].TaskID, got.Runs[1].TaskID)
	}
}

func TestEvalGetByID(t *testing.T) {
	mux, h := newEvalTestMux(t)

	seed := eval.NewRun(eval.KindPassK, "task-x", "m", 1)
	if err := h.store.Save(t.Context(), *seed); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/eval/runs/"+seed.ID, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var got eval.RunRecord
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.ID != seed.ID || got.TaskID != "task-x" {
			t.Fatalf("wrong record: %+v", got)
		}
	})

	t.Run("unknown id is 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/eval/runs/eval-missing", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "run not found") {
			t.Fatalf("body missing lowercase message: %s", rec.Body.String())
		}
	})

	t.Run("empty id is 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/eval/runs/", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestEvalRPCHandlersRoundtrip(t *testing.T) {
	h := newTestEvalHandler(t)
	handlers := h.EvalRPCHandlers()

	workdir := t.TempDir()

	t.Run("eval.run", func(t *testing.T) {
		params := fmt.Sprintf(`{"task_id":"rpc-task","model_id":"rpc-model","k":2,"command":"true","workdir":%q}`, workdir)
		out, err := handlers["eval.run"](t.Context(), json.RawMessage(params))
		if err != nil {
			t.Fatalf("eval.run: %v", err)
		}
		rec, ok := out.(*eval.RunRecord)
		if !ok {
			t.Fatalf("want *eval.RunRecord, got %T", out)
		}
		if !rec.Passed || len(rec.Attempts) != 2 {
			t.Fatalf("wrong run result: %+v", rec)
		}

		// eval.run validation errors.
		for name, bad := range map[string]string{
			"k zero":         `{"k":0}`,
			"no command":     `{"k":1,"workdir":"x"}`,
			"no workdir":     `{"k":1,"command":"true"}`,
			"malformed json": `{"k":`,
		} {
			if _, err := handlers["eval.run"](t.Context(), json.RawMessage(bad)); err == nil {
				t.Fatalf("%s: want error, got nil", name)
			}
		}
	})

	t.Run("eval.show", func(t *testing.T) {
		seed := eval.NewRun(eval.KindPassK, "show-task", "m", 1)
		if err := h.store.Save(t.Context(), *seed); err != nil {
			t.Fatalf("seed save: %v", err)
		}
		out, err := handlers["eval.show"](t.Context(), json.RawMessage(fmt.Sprintf(`{"id":%q}`, seed.ID)))
		if err != nil {
			t.Fatalf("eval.show: %v", err)
		}
		rec, ok := out.(eval.RunRecord)
		if !ok || rec.TaskID != "show-task" {
			t.Fatalf("wrong record: %+v", out)
		}

		if _, err := handlers["eval.show"](t.Context(), json.RawMessage(`{"id":"eval-gone"}`)); err == nil {
			t.Fatal("unknown id: want error, got nil")
		}
	})

	t.Run("eval.list", func(t *testing.T) {
		// Seed so the subtest is self-sufficient under -run filtering.
		seed := eval.NewRun(eval.KindPassK, "list-task", "m", 1)
		if err := h.store.Save(t.Context(), *seed); err != nil {
			t.Fatalf("seed save: %v", err)
		}
		out, err := handlers["eval.list"](t.Context(), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("eval.list: %v", err)
		}
		env, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("want envelope, got %T", out)
		}
		runs, ok := env["runs"].([]eval.RunRecord)
		if !ok {
			t.Fatalf("want runs slice, got %T", env["runs"])
		}
		if len(runs) < 1 {
			t.Fatalf("want >=1 run, got %d", len(runs))
		}
	})
}

func TestEvalRPCHandlersShareDiskStoreWithHTTP(t *testing.T) {
	mux, h := newEvalTestMux(t)
	handlers := h.EvalRPCHandlers()
	workdir := t.TempDir()

	// Run via HTTP...
	body := `{"task_id":"shared","model_id":"m","k":1,"command":"true","workdir":"` + workdir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/eval/runs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("http run status = %d", rec.Code)
	}
	var created eval.RunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// ...then read via RPC from the same store.
	out, err := handlers["eval.show"](t.Context(), json.RawMessage(fmt.Sprintf(`{"id":%q}`, created.ID)))
	if err != nil {
		t.Fatalf("rpc show after http run: %v", err)
	}
	if rec2, ok := out.(eval.RunRecord); !ok || rec2.ID != created.ID {
		t.Fatalf("rpc/HTTP store not shared: %+v", out)
	}

	// And the record file exists on disk at the documented shape.
	if _, err := os.Stat(filepath.Join(h.store.Dir, created.ID+".json")); err != nil {
		t.Fatalf("record file missing: %v", err)
	}
}
