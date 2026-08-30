package agent

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// stubTraceWriter satisfies TraceWriter and records what it was handed, so
// tests can assert on the mirror record the loop produces.
type stubTraceWriter struct {
	mu     sync.Mutex
	called bool
	rec    *traceRecordMirror
	err    error
}

func newStubTraceWriter(returnErr error) *stubTraceWriter {
	return &stubTraceWriter{err: returnErr}
}

func (s *stubTraceWriter) WriteTrace(rec *traceRecordMirror) (string, error) {
	s.mu.Lock()
	s.called = true
	s.rec = rec
	s.mu.Unlock()
	return "traces/2025-06-15/" + rec.ID + ".json", s.err
}

// snapshot returns (called, rec) under the stub lock.
func (s *stubTraceWriter) snapshot() (bool, *traceRecordMirror) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called, s.rec
}

// Compile-time assertion: *stubTraceWriter satisfies TraceWriter.
var _ TraceWriter = (*stubTraceWriter)(nil)

func TestBuildTraceRecord_FailureCarriesError(t *testing.T) {
	traj := Trajectory{ID: "conv-9", Domain: "code", Steps: []TrajectoryStep{{Action: "user_input", Input: "q"}}}
	rec := buildTraceRecord(traj, "conv-9", []string{"reviewer"}, fmt.Errorf("tool exploded"), "partial answer")
	if rec.Outcome != "failure" || rec.Error != "tool exploded" {
		t.Fatalf("failure not captured: %+v", rec)
	}
	if rec.ID == "" {
		t.Fatal("id must be generated")
	}
}

func TestBuildTraceRecord_SuccessCarriesSummaryAndSkills(t *testing.T) {
	traj := Trajectory{
		ID:     "conv-1",
		Domain: "code",
		Steps: []TrajectoryStep{
			{Action: "user_input", Input: "q", Success: true},
			{Action: "assistant_response", Output: "a", Success: true},
		},
	}
	created := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	rec := buildTraceRecord(traj, "conv-1", []string{"tester"}, nil, "done well")
	if rec.Outcome != "success" || rec.Error != "" {
		t.Fatalf("success record wrong: %+v", rec)
	}
	if rec.Summary != "done well" {
		t.Fatalf("summary not carried: %q", rec.Summary)
	}
	if len(rec.InjectedSkills) != 1 || rec.InjectedSkills[0] != "tester" {
		t.Fatalf("injected skills not carried: %v", rec.InjectedSkills)
	}
	if rec.Domain != "code" || rec.SessionID != "conv-1" {
		t.Fatalf("domain/session not carried: %+v", rec)
	}
	if len(rec.Steps) != 2 || rec.Steps[0].Action != "user_input" || rec.Steps[1].Action != "assistant_response" {
		t.Fatalf("steps not mirrored: %+v", rec.Steps)
	}
	if rec.CreatedAt.Before(created) {
		t.Fatalf("created_at predates test start: %v", rec.CreatedAt)
	}
}

func TestBuildTraceRecord_OverwriteTurnError(t *testing.T) {
	// A turn that failed after some good steps: every step carries the
	// turn-level outcome so sampled evidence is not misleadingly green.
	traj := Trajectory{ID: "conv-2", Steps: []TrajectoryStep{{Action: "assistant_response", Output: "a", Success: true}}}
	rec := buildTraceRecord(traj, "conv-2", nil, errors.New("late boom"), "")
	if rec.Steps[0].Success {
		t.Fatalf("step success must reflect turn error: %+v", rec.Steps[0])
	}
}

func TestBuildTraceRecord_NilStepsSafe(t *testing.T) {
	rec := buildTraceRecord(Trajectory{ID: "conv-3"}, "conv-3", nil, nil, "")
	if rec.Steps == nil {
		t.Fatal("steps must be non-nil so JSON emits [] not null")
	}
}

func TestWithTraceWriter_SetsAndNilGuards(t *testing.T) {
	var l AgentLoop
	stub := newStubTraceWriter(nil)
	WithTraceWriter(stub)(&l)
	if l.traceWriter == nil {
		t.Fatal("WithTraceWriter must set the traceWriter field")
	}
	// Nil guard: typed-nil and plain nil must not overwrite a good value.
	var typedNil *stubTraceWriter
	WithTraceWriter(typedNil)(&l)
	WithTraceWriter(nil)(&l)
	if l.traceWriter != TraceWriter(stub) {
		t.Fatal("nil options must not clobber an existing traceWriter")
	}
}

func TestAgentLoop_EmitsTraceOnFailedTurn(t *testing.T) {
	stub := newStubTraceWriter(nil)
	var l AgentLoop
	l.traceWriter = TraceWriter(stub)

	conv := NewConversation()
	conv.AddUserMessage("please do the thing")
	traj := l.buildTrajectory(conv, "conv-err", "")

	go func() {
		l.writeTraceHook(traj, "conv-err", []string{"reviewer"}, errors.New("reasoning failed"), "")
	}()
	if err := waitForTrace(stub, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	_, rec := stub.snapshot()
	if rec.Outcome != "failure" || rec.Error != "reasoning failed" {
		t.Fatalf("failure trace not captured: %+v", rec)
	}
	if rec.ID == "" || rec.SessionID != "conv-err" {
		t.Fatalf("trace identity wrong: %+v", rec)
	}
}

func TestAgentLoop_EmitsTraceOnSuccessfulTurn(t *testing.T) {
	stub := newStubTraceWriter(nil)
	var l AgentLoop
	l.traceWriter = TraceWriter(stub)

	conv := NewConversation()
	conv.AddUserMessage("hello")
	conv.AddAssistantMessage("hi there")
	traj := l.buildTrajectory(conv, "conv-ok", "hi there")

	go func() {
		l.writeTraceHook(traj, "conv-ok", nil, nil, "greeted")
	}()
	if err := waitForTrace(stub, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	_, rec := stub.snapshot()
	if rec.Outcome != "success" {
		t.Fatalf("success trace not captured: %+v", rec)
	}
}

func TestAgentLoop_TraceWriteErrorsNeverPropagate(t *testing.T) {
	stub := newStubTraceWriter(errors.New("disk full"))
	var l AgentLoop
	l.traceWriter = TraceWriter(stub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		l.writeTraceHook(Trajectory{ID: "conv-x"}, "conv-x", nil, errors.New("turn err"), "")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeTraceHook blocked on writer error")
	}
}

func TestAgentLoop_NilTraceWriterIsNoop(t *testing.T) {
	var l AgentLoop // traceWriter nil
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.writeTraceHook(Trajectory{ID: "conv-y"}, "conv-y", nil, errors.New("e"), "")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nil traceWriter must not block the hook")
	}
}

func waitForTrace(s *stubTraceWriter, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if called, _ := s.snapshot(); called {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("timed out waiting for WriteTrace")
}

// compile-time sanity: the mirror type is the shape TraceWriter consumes.
var _ = traceRecordMirror{}
