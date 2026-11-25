package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// TestStepHasErrorReadsSuccessFlag pins bughunt 2026-11-24 M9: the review
// envelope's "success" field was decoded but never read, so an explicit
// {"success":false} passed review as long as status/error were clean.
// An ABSENT success key is NOT a failure (legacy envelopes omit it).
func TestStepHasErrorReadsSuccessFlag(t *testing.T) {
	rm := newTestReviewManager(t)
	cases := []struct {
		name   string
		result string
		want   bool
	}{
		{"explicit success:false is an error", `{"status":"completed","success":false,"result":"wrote the file"}`, true},
		{"absent success key is not an error", `{"status":"completed","result":"wrote the file"}`, false},
		{"explicit success:true is not an error", `{"status":"completed","success":true,"result":"wrote the file"}`, false},
		{"status failed still errors", `{"status":"failed","success":true}`, true},
		{"error field still errors", `{"status":"completed","success":true,"error":"boom"}`, true},
	}
	for _, tc := range cases {
		step := &task.TaskStep{ID: "x", Result: tc.result}
		if got := rm.stepHasError(step); got != tc.want {
			t.Errorf("%s: stepHasError=%v, want %v", tc.name, got, tc.want)
		}
	}
}
