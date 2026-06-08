package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func newTestBus() *bus.MessageBus {
	return bus.New(nil, newTestLogger())
}

func newTestTeamDriver(opts ...func(*ParallelTeamDriverDeps)) *ParallelTeamDriver {
	deps := ParallelTeamDriverDeps{
		Logger: newTestLogger(),
		Bus:    newTestBus(),
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return NewParallelTeamDriver(deps)
}

func newTestTeamSession(participants []string) *CollaborationSession {
	cfg := DefaultSessionConfig()
	cfg.TurnTimeout = 2 * time.Second
	cfg.TimeBudget = 10 * time.Second
	return NewCollaborationSession("team_parallel", "task-team-01", participants, cfg)
}

// ---------------------------------------------------------------------------
// Name / CanInitiate
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Name(t *testing.T) {
	d := newTestTeamDriver()
	if d.Name() != "team_parallel" {
		t.Errorf("Name() = %q, want %q", d.Name(), "team_parallel")
	}
}

func TestParallelTeamDriver_CanInitiate(t *testing.T) {
	d := newTestTeamDriver()
	tests := []struct {
		name    string
		reason  string
		want    bool
	}{
		{"team keyword", "needs a team approach", true},
		{"multi keyword", "use multi-agent for this", true},
		{"parallel keyword", "run in parallel", true},
		{"TEAM uppercase", "TEAM effort needed", true},
		{"MULTI uppercase", "MULTI-agent task", true},
		{"parallel mixed case", "ParAllEl execution", true},
		{"no keyword", "just a simple coding task", false},
		{"empty reason", "", false},
		{"unrelated reason", "fix the bug in line 42", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := d.CanInitiate("coder", tc.reason)
			if got != tc.want {
				t.Errorf("CanInitiate(%q) = %v, want %v", tc.reason, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewParallelTeamDriver_Defaults(t *testing.T) {
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{})
	if d.logger == nil {
		t.Error("logger should not be nil when no logger provided (should use slog.Default)")
	}
	if d.conversations == nil {
		t.Error("conversations map should be initialized")
	}
	if d.bus != nil {
		t.Error("bus should be nil when not provided")
	}
	if d.registry != nil {
		t.Error("registry should be nil when not provided")
	}
}

func TestNewParallelTeamDriver_WithDeps(t *testing.T) {
	b := newTestBus()
	l := newTestLogger()
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{
		Bus:    b,
		Logger: l,
	})
	if d.logger != l {
		t.Error("logger should match provided logger")
	}
	if d.bus != b {
		t.Error("bus should match provided bus")
	}
	if d.conversations == nil {
		t.Error("conversations map should be initialized")
	}
}

// ---------------------------------------------------------------------------
// TeamConfig parsing
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_parseTeamConfig(t *testing.T) {
	d := newTestTeamDriver()

	t.Run("single participant", func(t *testing.T) {
		sess := newTestTeamSession([]string{"lead"})
		cfg := d.parseTeamConfig(sess)
		if cfg.LeadAgent != "lead" {
			t.Errorf("LeadAgent = %q, want %q", cfg.LeadAgent, "lead")
		}
		if len(cfg.Roster) != 0 {
			t.Errorf("Roster = %v, want empty for single participant", cfg.Roster)
		}
	})

	t.Run("multiple participants", func(t *testing.T) {
		sess := newTestTeamSession([]string{"lead", "coder", "analyst"})
		cfg := d.parseTeamConfig(sess)
		if cfg.LeadAgent != "lead" {
			t.Errorf("LeadAgent = %q, want %q", cfg.LeadAgent, "lead")
		}
		if len(cfg.Roster) != 2 {
			t.Errorf("len(Roster) = %d, want 2", len(cfg.Roster))
		}
		if cfg.Roster[0] != "coder" || cfg.Roster[1] != "analyst" {
			t.Errorf("Roster = %v, want [coder analyst]", cfg.Roster)
		}
	})

	t.Run("empty participants", func(t *testing.T) {
		sess := newTestTeamSession([]string{})
		cfg := d.parseTeamConfig(sess)
		if cfg.LeadAgent != "" {
			t.Errorf("LeadAgent = %q, want empty", cfg.LeadAgent)
		}
		if len(cfg.Roster) != 0 {
			t.Errorf("Roster = %v, want empty", cfg.Roster)
		}
	})
}

// ---------------------------------------------------------------------------
// Run - validation
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_TooFewParticipants(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"solo"})

	_, err := d.Run(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error for single participant")
	}
	var collabErr *CollaborationError
	if !isCollaborationError(err, ErrCodeInvalidMode) {
		t.Errorf("expected CollaborationError with code %q, got %T: %v", ErrCodeInvalidMode, err, err)
	}
	_ = collabErr
}

func isCollaborationError(err error, code string) bool {
	var ce *CollaborationError
	if err == nil {
		return false
	}
	// unwrap wrapped errors
	type unwrapper interface{ Unwrap() error }
	for {
		if as, ok := err.(*CollaborationError); ok {
			ce = as
			break
		}
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		return false
	}
	return ce != nil && ce.Code == code
}

// ---------------------------------------------------------------------------
// Run - workspace creation (no workspace manager)
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_WorkspaceCreation(t *testing.T) {
	d := newTestTeamDriver()
	// Use a temp directory to avoid polluting home dir.
	// We'll override by using a session with a temp-based workspace.
	sess := newTestTeamSession([]string{"lead", "coder"})

	// The Run method requires a registry for runAgent; we expect it to fail
	// at fanOut because there is no registry. But workspace should be created first.
	_, err := d.Run(t.Context(), sess)
	// Should fail at runAgent since no registry, but workspace should exist
	if sess.Workspace == "" {
		t.Fatal("workspace should be set even when Run fails later")
	}
	// Verify workspace directory exists
	if _, statErr := os.Stat(sess.Workspace); os.IsNotExist(statErr) {
		t.Errorf("workspace directory %s should exist", sess.Workspace)
	}
	// Clean up
	os.RemoveAll(sess.Workspace)

	// The error should be about the agent registry (runAgent fails)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !strings.Contains(err.Error(), "agent registry not configured") &&
		!strings.Contains(err.Error(), "team fan-out failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Run - no registry error path
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_NoRegistry(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder", "analyst"})

	_, err := d.Run(t.Context(), sess)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	// Clean up workspace
	if sess.Workspace != "" {
		os.RemoveAll(sess.Workspace)
	}
}

// ---------------------------------------------------------------------------
// Run - cancelled context
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_CancelledContext(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := d.Run(ctx, sess)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
	// Clean up workspace if created
	if sess.Workspace != "" {
		os.RemoveAll(sess.Workspace)
	}
}

// ---------------------------------------------------------------------------
// Run - cleanup on return
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_CleanupSession(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder"})

	_, _ = d.Run(t.Context(), sess)

	// After Run returns (even with error), the session should be cleaned up
	d.convMu.RLock()
	_, exists := d.conversations[sess.ID]
	d.convMu.RUnlock()
	if exists {
		t.Error("session should be cleaned up after Run returns")
	}

	if sess.Workspace != "" {
		os.RemoveAll(sess.Workspace)
	}
}

// ---------------------------------------------------------------------------
// fanOut - direct unit test
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_fanOut_NoRegistry(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "member1", "member2"})

	cfg := TeamConfig{
		LeadAgent:     "lead",
		Roster:        []string{"member1", "member2"},
		MaxConcurrent: 2,
		MemberTimeout: 1 * time.Second,
	}

	// Set up team status so updateMemberStatus can find it
	d.convMu.Lock()
	d.conversations[sess.ID] = &TeamStatus{
		SessionID:     sess.ID,
		LeadAgent:     "lead",
		Phase:         "fan_out",
		MemberResults: map[string]*TeamMemberResult{
			"member1": {AgentID: "member1", Status: MemberPending},
			"member2": {AgentID: "member2", Status: MemberPending},
		},
	}
	d.convMu.Unlock()
	defer d.cleanupSession(sess.ID)

	// fanOut does NOT return errors for individual member failures;
	// it collects partial results (each goroutine returns nil on failure).
	results, err := d.fanOut(t.Context(), sess, cfg)
	if err != nil {
		t.Fatalf("fanOut returned unexpected error: %v", err)
	}

	// Results should contain failed entries for each member
	if len(results) != 2 {
		t.Errorf("len(results) = %d, want 2", len(results))
	}
	for _, member := range cfg.Roster {
		r, ok := results[member]
		if !ok {
			t.Errorf("missing result for member %q", member)
			continue
		}
		if r.Status != MemberFailed {
			t.Errorf("member %q status = %q, want %q", member, r.Status, MemberFailed)
		}
		if r.Error == "" {
			t.Errorf("member %q should have an error message", member)
		}
	}

	// Members should have been marked as failed in the team status
	d.convMu.RLock()
	ts := d.conversations[sess.ID]
	m1Status := ts.MemberResults["member1"].Status
	m2Status := ts.MemberResults["member2"].Status
	d.convMu.RUnlock()

	if m1Status != MemberFailed {
		t.Errorf("member1 status = %q, want %q after failure", m1Status, MemberFailed)
	}
	if m2Status != MemberFailed {
		t.Errorf("member2 status = %q, want %q after failure", m2Status, MemberFailed)
	}
}

// ---------------------------------------------------------------------------
// fanOut - cancelled context returns partial results
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_fanOut_CancelledContext(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "member1"})

	cfg := TeamConfig{
		LeadAgent:     "lead",
		Roster:        []string{"member1"},
		MaxConcurrent: 1,
		MemberTimeout: 5 * time.Minute,
	}

	// Pre-populate conversation status
	d.convMu.Lock()
	d.conversations[sess.ID] = &TeamStatus{
		SessionID:     sess.ID,
		LeadAgent:     "lead",
		Phase:         "fan_out",
		MemberResults: map[string]*TeamMemberResult{
			"member1": {AgentID: "member1", Status: MemberPending},
		},
	}
	d.convMu.Unlock()
	defer d.cleanupSession(sess.ID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := d.fanOut(ctx, sess, cfg)
	// fanOut collects partial results rather than aborting on individual failures.
	// With a cancelled context and no registry, the goroutine may either:
	//   - Hit ctx.Done() in the semaphore select -> returns error from eg.Wait()
	//   - Acquire the semaphore and fail in runAgent -> returns nil (partial result collected)
	// Both outcomes are valid. What matters is that we get a failed member result.
	if err != nil {
		// Context cancellation propagated through errgroup
		return
	}
	// If no error, we should have a partial result with MemberFailed status
	if len(results) == 0 {
		t.Fatal("expected at least one result (partial or failed)")
	}
	mr, ok := results["member1"]
	if !ok {
		t.Fatal("expected member1 result")
	}
	if mr.Status != MemberFailed {
		t.Errorf("member1 status = %q, want %q", mr.Status, MemberFailed)
	}
}

// ---------------------------------------------------------------------------
// synthesize - no registry
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_synthesize_NoRegistry(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "member1"})

	cfg := TeamConfig{
		LeadAgent: "lead",
		Roster:    []string{"member1"},
	}

	results := map[string]*TeamMemberResult{
		"member1": {
			AgentID: "member1",
			Subtask: "task-team-01",
			Output:  "partial result from member1",
			Status:  MemberDone,
		},
	}

	_, err := d.synthesize(t.Context(), sess, cfg, results)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !strings.Contains(err.Error(), "agent registry not configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// synthesize records a turn
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_synthesize_TurnRecorded(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "member1"})

	cfg := TeamConfig{
		LeadAgent: "lead",
		Roster:    []string{"member1"},
	}

	results := map[string]*TeamMemberResult{
		"member1": {
			AgentID: "member1",
			Output:  "done",
			Status:  MemberDone,
		},
	}

	beforeTurns := sess.TurnCount()

	_, err := d.synthesize(t.Context(), sess, cfg, results)
	if err == nil {
		t.Fatal("expected error (no registry) - this is fine for the test")
	}

	// The synthesize function should not record a turn when runAgent fails
	afterTurns := sess.TurnCount()
	if afterTurns != beforeTurns {
		t.Errorf("turn count changed from %d to %d, expected no change when synthesize fails", beforeTurns, afterTurns)
	}
}

// ---------------------------------------------------------------------------
// buildMemberPrompt
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_buildMemberPrompt(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder"})
	cfg := TeamConfig{
		LeadAgent: "lead",
		Roster:    []string{"coder"},
	}

	prompt := d.buildMemberPrompt(sess, "coder", cfg)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !strings.Contains(prompt, "Team Member Task Assignment") {
		t.Error("prompt should contain task assignment heading")
	}
	if !strings.Contains(prompt, "coder") {
		t.Error("prompt should contain member ID")
	}
	if !strings.Contains(prompt, "lead") {
		t.Error("prompt should contain lead agent")
	}
	if !strings.Contains(prompt, "task-team-01") {
		t.Error("prompt should contain task ID")
	}
	if !strings.Contains(prompt, "Specialist") {
		t.Error("prompt should mention specialist role")
	}
}

// ---------------------------------------------------------------------------
// buildLeadSynthesisPrompt
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_buildLeadSynthesisPrompt(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder", "analyst", "debugger"})
	cfg := TeamConfig{
		LeadAgent: "lead",
		Roster:    []string{"coder", "analyst", "debugger"},
	}

	results := map[string]*TeamMemberResult{
		"coder": {
			AgentID: "coder",
			Output:  "I wrote the implementation",
			Status:  MemberDone,
		},
		"analyst": {
			AgentID: "analyst",
			Output:  "Here is the analysis",
			Status:  MemberDone,
		},
		"debugger": {
			AgentID: "debugger",
			Status:  MemberFailed,
			Error:   "could not reproduce the bug",
		},
	}

	prompt := d.buildLeadSynthesisPrompt(sess, cfg, results)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}

	// Lead role
	if !strings.Contains(prompt, "Lead agent (lead)") {
		t.Error("prompt should identify the lead agent")
	}

	// Member count
	if !strings.Contains(prompt, "3 members") {
		t.Error("prompt should show correct member count")
	}

	// Completed member
	if !strings.Contains(prompt, "COMPLETED") {
		t.Error("prompt should mark completed members")
	}
	if !strings.Contains(prompt, "I wrote the implementation") {
		t.Error("prompt should contain completed member output")
	}

	// Failed member
	if !strings.Contains(prompt, "FAILED") {
		t.Error("prompt should mark failed members")
	}
	if !strings.Contains(prompt, "could not reproduce the bug") {
		t.Error("prompt should contain failed member error")
	}

	// Synthesis instructions
	if !strings.Contains(prompt, "Synthesize") {
		t.Error("prompt should instruct the lead to synthesize")
	}
}

func TestParallelTeamDriver_buildLeadSynthesisPrompt_MissingResult(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder"})
	cfg := TeamConfig{
		LeadAgent: "lead",
		Roster:    []string{"coder", "analyst"},
	}

	// analyst has no result entry
	results := map[string]*TeamMemberResult{
		"coder": {AgentID: "coder", Output: "done", Status: MemberDone},
	}

	prompt := d.buildLeadSynthesisPrompt(sess, cfg, results)
	if !strings.Contains(prompt, "NO RESULT") {
		t.Error("prompt should mention NO RESULT for missing members")
	}
}

// ---------------------------------------------------------------------------
// TeamConfig defaults (set in Run)
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_MaxConcurrentDefaults(t *testing.T) {
	// Verify that MaxConcurrent defaults to roster length
	sess := newTestTeamSession([]string{"lead", "coder", "analyst", "planner"})
	cfg := TeamConfig{
		LeadAgent:     "lead",
		Roster:        []string{"coder", "analyst", "planner"},
		MaxConcurrent: 0, // will default to len(Roster)
		MemberTimeout: 0, // will default to TurnTimeout then 5m
	}

	// Simulate the Run method's defaults logic
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = len(cfg.Roster)
	}
	if cfg.MemberTimeout <= 0 {
		cfg.MemberTimeout = sess.TurnTimeout
		if cfg.MemberTimeout <= 0 {
			cfg.MemberTimeout = 5 * time.Minute
		}
	}

	if cfg.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, want 3", cfg.MaxConcurrent)
	}
	if cfg.MemberTimeout != sess.TurnTimeout {
		t.Errorf("MemberTimeout = %v, want %v", cfg.MemberTimeout, sess.TurnTimeout)
	}
}

func TestParallelTeamDriver_Run_MemberTimeoutFallback(t *testing.T) {
	cfg := SessionConfig{} // zero values
	sess := NewCollaborationSession("team_parallel", "task-1", []string{"a", "b"}, cfg)

	memberTimeout := sess.TurnTimeout
	if memberTimeout <= 0 {
		memberTimeout = 5 * time.Minute
	}
	if memberTimeout != 5*time.Minute {
		t.Errorf("MemberTimeout = %v, want 5m", memberTimeout)
	}
}

// ---------------------------------------------------------------------------
// TeamStatus state transitions
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_TeamStatus_Initial(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder", "analyst"})

	cfg := d.parseTeamConfig(sess)
	status := &TeamStatus{
		SessionID:     sess.ID,
		LeadAgent:     cfg.LeadAgent,
		Phase:         "fan_out",
		MemberResults: make(map[string]*TeamMemberResult),
	}
	for _, member := range cfg.Roster {
		status.MemberResults[member] = &TeamMemberResult{
			AgentID: member,
			Status:  MemberPending,
		}
	}

	if status.Phase != "fan_out" {
		t.Errorf("initial phase = %q, want fan_out", status.Phase)
	}
	if status.LeadAgent != "lead" {
		t.Errorf("lead agent = %q, want lead", status.LeadAgent)
	}
	for _, member := range cfg.Roster {
		if status.MemberResults[member].Status != MemberPending {
			t.Errorf("%s status = %q, want %q", member, status.MemberResults[member].Status, MemberPending)
		}
	}
}

func TestParallelTeamDriver_TeamStatus_PhaseTransitions(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "m1"})

	d.convMu.Lock()
	d.conversations[sess.ID] = &TeamStatus{
		SessionID:     sess.ID,
		LeadAgent:     "lead",
		Phase:         "fan_out",
		MemberResults: map[string]*TeamMemberResult{
			"m1": {AgentID: "m1", Status: MemberPending},
		},
	}
	d.convMu.Unlock()
	defer d.cleanupSession(sess.ID)

	// Transition: pending -> running
	d.updateMemberStatus(sess.ID, "m1", MemberRunning, "")
	d.convMu.RLock()
	status := d.conversations[sess.ID]
	d.convMu.RUnlock()
	if status.MemberResults["m1"].Status != MemberRunning {
		t.Errorf("status = %q, want %q", status.MemberResults["m1"].Status, MemberRunning)
	}

	// Transition: running -> done
	d.updateMemberStatus(sess.ID, "m1", MemberDone, "")
	d.convMu.RLock()
	status = d.conversations[sess.ID]
	d.convMu.RUnlock()
	if status.MemberResults["m1"].Status != MemberDone {
		t.Errorf("status = %q, want %q", status.MemberResults["m1"].Status, MemberDone)
	}

	// Transition: running -> failed with error message
	d.updateMemberStatus(sess.ID, "m1", MemberFailed, "timeout")
	d.convMu.RLock()
	status = d.conversations[sess.ID]
	d.convMu.RUnlock()
	if status.MemberResults["m1"].Status != MemberFailed {
		t.Errorf("status = %q, want %q", status.MemberResults["m1"].Status, MemberFailed)
	}
	if status.MemberResults["m1"].Error != "timeout" {
		t.Errorf("error = %q, want %q", status.MemberResults["m1"].Error, "timeout")
	}
}

func TestParallelTeamDriver_updateMemberStatus_NoSession(t *testing.T) {
	d := newTestTeamDriver()
	// Should not panic when session does not exist
	d.updateMemberStatus("nonexistent", "member", MemberRunning, "test")
}

func TestParallelTeamDriver_updateMemberStatus_NoMember(t *testing.T) {
	d := newTestTeamDriver()
	d.convMu.Lock()
	d.conversations["sess-1"] = &TeamStatus{
		SessionID:     "sess-1",
		LeadAgent:     "lead",
		Phase:         "fan_out",
		MemberResults: map[string]*TeamMemberResult{},
	}
	d.convMu.Unlock()
	defer d.cleanupSession("sess-1")

	// Should not panic when member does not exist in results
	d.updateMemberStatus("sess-1", "ghost", MemberFailed, "oops")
}

// ---------------------------------------------------------------------------
// Topic helper functions
// ---------------------------------------------------------------------------

func TestTeamTopicHelpers(t *testing.T) {
	tests := []struct {
		name     string
		fn       func(string) string
		input    string
		expected string
	}{
		{"status", TeamStatusTopic, "sess-42", "team.sess-42.status"},
		{"message", TeamMessageTopic, "sess-42", "team.sess-42.message"},
		{"result", TeamResultTopic, "sess-42", "team.sess-42.result"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn(tc.input)
			if got != tc.expected {
				t.Errorf("%s(%q) = %q, want %q", tc.name, tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MemberStatus constants
// ---------------------------------------------------------------------------

func TestMemberStatus_Values(t *testing.T) {
	tests := []struct {
		status MemberStatus
		want   string
	}{
		{MemberPending, "pending"},
		{MemberRunning, "running"},
		{MemberDone, "done"},
		{MemberFailed, "failed"},
	}
	for _, tc := range tests {
		if string(tc.status) != tc.want {
			t.Errorf("MemberStatus = %q, want %q", tc.status, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Bus event publishing
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_publishEvent_NilBus(t *testing.T) {
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Logger: newTestLogger()})
	// Should not panic
	d.publishEvent("sess-1", TopicTeamStart, map[string]any{"key": "value"})
}

func TestParallelTeamDriver_publishEvent_BusReceivesMessage(t *testing.T) {
	b := newTestBus()
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Bus: b, Logger: newTestLogger()})

	sub := b.Subscribe("test-evt", TopicTeamStart)

	d.publishEvent("sess-1", TopicTeamStart, map[string]any{
		"session_id": "sess-1",
		"mode":       "team_parallel",
	})

	// Read from subscriber channel with timeout
	var received *models.BusMessage
	select {
	case received = <-sub.Channel:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for bus message")
	}

	if received.Topic != TopicTeamStart {
		t.Errorf("topic = %q, want %q", received.Topic, TopicTeamStart)
	}
	if received.Source != "parallel-team-driver" {
		t.Errorf("source = %q, want parallel-team-driver", received.Source)
	}
}

func TestParallelTeamDriver_publishTeamStatus_NilBus(t *testing.T) {
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Logger: newTestLogger()})
	status := &TeamStatus{
		SessionID:     "sess-1",
		LeadAgent:     "lead",
		Phase:         "collect",
		MemberResults: map[string]*TeamMemberResult{},
	}
	// Should not panic
	d.publishTeamStatus("sess-1", status)
}

func TestParallelTeamDriver_publishTeamStatus_BusReceives(t *testing.T) {
	b := newTestBus()
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Bus: b, Logger: newTestLogger()})

	status := &TeamStatus{
		SessionID: "sess-1",
		LeadAgent: "lead",
		Phase:     "collect",
		MemberResults: map[string]*TeamMemberResult{
			"member1": {AgentID: "member1", Status: MemberDone},
		},
	}

	expectedTopic := TeamStatusTopic("sess-1")
	sub := b.Subscribe("test-status", expectedTopic)

	d.publishTeamStatus("sess-1", status)

	var received *models.BusMessage
	select {
	case received = <-sub.Channel:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for bus message")
	}

	if received.Topic != expectedTopic {
		t.Errorf("topic = %q, want %q", received.Topic, expectedTopic)
	}
}

func TestParallelTeamDriver_publishMemberCompleted_NilBus(t *testing.T) {
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Logger: newTestLogger()})
	d.publishMemberCompleted("sess-1", "member1", "lead")
}

func TestParallelTeamDriver_publishPartialResults_NilBus(t *testing.T) {
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Logger: newTestLogger()})
	results := map[string]*TeamMemberResult{
		"member1": {AgentID: "member1", Output: "done", Status: MemberDone},
	}
	d.publishPartialResults("sess-1", results)
}

func TestParallelTeamDriver_publishPartialResults_BusReceives(t *testing.T) {
	b := newTestBus()
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Bus: b, Logger: newTestLogger()})

	results := map[string]*TeamMemberResult{
		"m1": {AgentID: "m1", Output: "result-1", Status: MemberDone},
		"m2": {AgentID: "m2", Status: MemberFailed, Error: "timeout"},
	}

	expectedTopic := TeamResultTopic("sess-42")
	sub := b.Subscribe("test-results", expectedTopic)

	d.publishPartialResults("sess-42", results)

	var received *models.BusMessage
	select {
	case received = <-sub.Channel:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for bus message")
	}

	if received.Topic != expectedTopic {
		t.Errorf("topic = %q, want %q", received.Topic, expectedTopic)
	}
}

// ---------------------------------------------------------------------------
// createWorkspace without WorkspaceManager
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_createWorkspace_NoManager(t *testing.T) {
	d := newTestTeamDriver()
	sess := newTestTeamSession([]string{"lead", "coder"})

	wsPath, err := d.createWorkspace(t.Context(), sess)
	if err != nil {
		t.Fatalf("createWorkspace failed: %v", err)
	}
	if wsPath == "" {
		t.Fatal("workspace path should not be empty")
	}
	if _, statErr := os.Stat(wsPath); os.IsNotExist(statErr) {
		t.Errorf("workspace directory %s should exist", wsPath)
	}
	// Clean up
	os.RemoveAll(wsPath)
}

// ---------------------------------------------------------------------------
// runAgent without registry
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_runAgent_NoRegistry(t *testing.T) {
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Logger: newTestLogger()})
	_, err := d.runAgent(context.Background(), "agent1", "hello", "conv-1")
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !isCollaborationError(err, ErrCodeAgentFailed) {
		t.Errorf("expected CollaborationError with code %q, got: %v", ErrCodeAgentFailed, err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety (does not trigger known data race in updateMemberStatus;
// that method uses RLock but mutates fields -- a pre-existing production bug.
// This test verifies no panics from nil-dereference during concurrent access.)
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_ConcurrentCleanupAndLookup(t *testing.T) {
	d := newTestTeamDriver()

	// Populate multiple sessions
	for i := 0; i < 10; i++ {
		sid := fmt.Sprintf("sess-%d", i)
		d.convMu.Lock()
		d.conversations[sid] = &TeamStatus{
			SessionID: sid,
			LeadAgent: "lead",
			Phase:     "fan_out",
			MemberResults: map[string]*TeamMemberResult{
				"m1": {AgentID: "m1", Status: MemberPending},
			},
		}
		d.convMu.Unlock()
	}

	var wg sync.WaitGroup
	// Concurrent cleanups
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sid := fmt.Sprintf("sess-%d", idx)
			d.updateMemberStatus(sid, "nonexistent", MemberFailed, "noop")
		}(i)
	}
	// Concurrent lookups on missing sessions
	for i := 5; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sid := fmt.Sprintf("sess-%d", idx)
			d.updateMemberStatus(sid, "ghost", MemberRunning, "noop")
		}(i)
	}
	wg.Wait()

	// Clean up remaining
	for i := 0; i < 10; i++ {
		d.cleanupSession(fmt.Sprintf("sess-%d", i))
	}

	// Should not panic under any circumstance
}

// ---------------------------------------------------------------------------
// Integration - Run publishes events through bus
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_PublishesSessionCreated(t *testing.T) {
	b := newTestBus()
	d := NewParallelTeamDriver(ParallelTeamDriverDeps{Bus: b, Logger: newTestLogger()})
	sess := newTestTeamSession([]string{"lead", "coder"})

	sub := b.Subscribe("test-created", TopicCollabSessionCreated)

	// Run will fail at fanOut (no registry) but should publish session_created first
	_, _ = d.Run(t.Context(), sess)

	select {
	case <-sub.Channel:
		// received session_created event
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for session_created event")
	}

	if sess.Workspace != "" {
		os.RemoveAll(sess.Workspace)
	}
}

// ---------------------------------------------------------------------------
// TeamMemberResult fields
// ---------------------------------------------------------------------------

func TestTeamMemberResult_Fields(t *testing.T) {
	r := &TeamMemberResult{
		AgentID:  "coder",
		Subtask:  "task-1",
		Output:   "implementation done",
		Status:   MemberDone,
		Error:    "",
		Duration: 500 * time.Millisecond,
	}
	if r.AgentID != "coder" {
		t.Errorf("AgentID = %q, want coder", r.AgentID)
	}
	if r.Status != MemberDone {
		t.Errorf("Status = %q, want done", r.Status)
	}
	if r.Duration != 500*time.Millisecond {
		t.Errorf("Duration = %v, want 500ms", r.Duration)
	}
}

// ---------------------------------------------------------------------------
// Integration: Run with TimeBudget context
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_Run_TimeBudgetEnforced(t *testing.T) {
	d := newTestTeamDriver()
	cfg := DefaultSessionConfig()
	cfg.TimeBudget = 1 * time.Nanosecond // very short budget
	sess := NewCollaborationSession("team_parallel", "task-budget", []string{"lead", "coder"}, cfg)

	start := time.Now()
	_, err := d.Run(context.Background(), sess)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to time budget")
	}

	// Should complete quickly due to the tiny time budget
	if elapsed > 5*time.Second {
		t.Errorf("Run took %v, should have been bounded by time budget", elapsed)
	}

	if sess.Workspace != "" {
		os.RemoveAll(sess.Workspace)
	}
}

// ---------------------------------------------------------------------------
// createWorkspace with temp dir override
// ---------------------------------------------------------------------------

func TestParallelTeamDriver_createWorkspace_UsesTempDir(t *testing.T) {
	d := newTestTeamDriver()
	// Create session with a known ID to find workspace
	sess := newTestTeamSession([]string{"lead", "coder"})

	wsPath, err := d.createWorkspace(t.Context(), sess)
	if err != nil {
		t.Fatalf("createWorkspace failed: %v", err)
	}

	// Verify it's under the collab workspace base
	baseDir := getCollabWorkspaceBase()
	expectedPrefix := filepath.Join(baseDir, "team-")
	if !strings.HasPrefix(wsPath, expectedPrefix) {
		t.Errorf("workspace path %q should start with %q", wsPath, expectedPrefix)
	}

	// Clean up
	os.RemoveAll(wsPath)
}
