package eval

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewRun(t *testing.T) {
	r := NewRun(KindModelSwap, "task-1", "model-a", 3)
	if r.ID == "" {
		t.Error("NewRun must set a non-empty ID")
	}
	if !strings.HasPrefix(r.ID, "eval-") {
		t.Errorf("ID %q should carry the eval- prefix", r.ID)
	}
	if r.Kind != KindModelSwap {
		t.Errorf("Kind = %q, want %q", r.Kind, KindModelSwap)
	}
	if r.TaskID != "task-1" || r.ModelID != "model-a" || r.K != 3 {
		t.Errorf("fields not preserved: %+v", r)
	}
	if r.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt should be UTC, got %v", r.CreatedAt.Location())
	}
	if r.Attempts == nil || len(r.Attempts) != 0 {
		t.Errorf("Attempts should initialize empty, got %#v", r.Attempts)
	}
}

func TestRunRecordAddAttempt(t *testing.T) {
	r := NewRun(KindPassK, "t", "m", 1)
	r.AddAttempt(Attempt{Index: 0, ModelID: "m", Passed: true})
	r.AddAttempt(Attempt{Index: 1, ModelID: "m", Passed: false})
	if len(r.Attempts) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(r.Attempts))
	}
	if r.Attempts[1].Index != 1 || r.Attempts[1].Passed {
		t.Errorf("attempt not stored faithfully: %+v", r.Attempts[1])
	}
}

func TestRunRecordJSONRoundtrip(t *testing.T) {
	created := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	r := &RunRecord{
		ID:          "eval-abc123",
		CreatedAt:   created,
		Kind:        KindAblation,
		TaskID:      "task-42",
		HarnessHash: "deadbeef",
		ModelID:     "model-x",
		K:           3,
		Attempts: []Attempt{{
			Index:        0,
			ModelID:      "model-x",
			Passed:       true,
			Oracle:       OracleResult{Passed: true, Output: "ok"},
			TrajectoryID: "traj-1",
		}},
		Passed:     true,
		OracleName: "shell",
	}

	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// C1 field names, exact snake_case.
	for _, want := range []string{
		`"id":`, `"created_at":`, `"kind":`, `"task_id":`, `"harness_hash":`,
		`"model_id":`, `"k":`, `"attempts":`, `"passed":`, `"oracle_name":`,
		`"index":`, `"trajectory_id":`, `"output":`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON missing field %s in %s", want, raw)
		}
	}
	// `error` is omitempty and the fixture oracle passed, so it must be absent.
	if strings.Contains(string(raw), `"error":`) {
		t.Errorf("empty error must be omitted: %s", raw)
	}
	for _, banned := range []string{"taskID", "harnessHash", "modelID", "oracleName", "trajectoryID"} {
		if strings.Contains(string(raw), `"`+banned+`"`) {
			t.Errorf("JSON contains camelCase field %q: %s", banned, raw)
		}
	}

	var got RunRecord
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Attempts) != 1 || got.Attempts[0] != r.Attempts[0] {
		t.Errorf("attempts roundtrip mismatch: %+v", got.Attempts)
	}
	if got.ID != r.ID || got.Kind != r.Kind || got.TaskID != r.TaskID ||
		got.HarnessHash != r.HarnessHash || got.ModelID != r.ModelID ||
		got.K != r.K || got.Passed != r.Passed || got.OracleName != r.OracleName {
		t.Errorf("scalar fields roundtrip mismatch:\n got %+v\nwant %+v", got, *r)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt roundtrip mismatch: %v vs %v", got.CreatedAt, created)
	}
}

func TestRunRecordOmitEmptyFields(t *testing.T) {
	raw, err := json.Marshal(&RunRecord{ID: "eval-1", CreatedAt: time.Now().UTC(), Kind: KindPassK})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "trajectory_id") {
		t.Errorf("empty trajectory_id must be omitted: %s", raw)
	}
}
