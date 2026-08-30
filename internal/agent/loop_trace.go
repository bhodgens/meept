package agent

import (
	"log/slog"
	"reflect"
	"time"

	pkgid "github.com/caimlas/meept/pkg/id"
)

// traceRecordMirror mirrors the JSON shape of selfimprove.TraceRecord without
// importing internal/selfimprove (the agent package must not depend on it).
// Field order, names, and JSON tags must stay in lockstep with the store side.
type traceRecordMirror struct {
	ID             string            `json:"id"`
	SessionID      string            `json:"session_id"`
	Domain         string            `json:"domain,omitempty"`
	Outcome        string            `json:"outcome"`
	Error          string            `json:"error,omitempty"`
	InjectedSkills []string          `json:"injected_skills,omitempty"`
	Steps          []traceStepMirror `json:"steps"`
	Summary        string            `json:"summary,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

// traceStepMirror mirrors selfimprove.TraceStep's JSON shape.
type traceStepMirror struct {
	Action  string `json:"action"`
	Input   string `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
	Success bool   `json:"success"`
}

// TraceWriter is the narrow persistence hook the loop uses to persist turn
// traces. Implemented by *selfimprove.TraceStore (wired in leaf 05 via
// selfimprove.NewTraceStore) — the agent package itself never imports
// selfimprove.
type TraceWriter interface {
	WriteTrace(rec *traceRecordMirror) (string, error)
}

// WithTraceWriter sets the loop's trace persistence hook. Every
// learning-eligible turn (success AND failure, when a conversation snapshot is
// available) writes a trace through it. Nil guard covers both an untyped nil
// interface and a typed-nil writer (a nil *T stored in the interface), which
// would otherwise panic when called.
func WithTraceWriter(tw TraceWriter) LoopOption {
	return func(l *AgentLoop) {
		if tw == nil {
			return
		}
		if v := reflect.ValueOf(tw); v.Kind() == reflect.Pointer && v.IsNil() {
			return
		}
		l.mu.Lock()
		l.traceWriter = tw
		l.mu.Unlock()
	}
}

// traceOutcomeSuccess / traceOutcomeFailure mirror the store-side constants so
// the mirror record's Outcome values match selfimprove.TraceOutcome* without
// an import.
const (
	traceOutcomeSuccess = "success"
	traceOutcomeFailure = "failure"
)

// buildTraceRecord converts a learning trajectory into the mirror trace
// record. turnErr decides the outcome: non-nil means Outcome=failure with the
// error text (even if individual steps report Success — step flags are
// overwritten to reflect the turn-level outcome so sampled evidence is not
// misleadingly green); nil means success.
func buildTraceRecord(traj Trajectory, sessionID string, injectedSkills []string, turnErr error, summary string) *traceRecordMirror {
	outcome := traceOutcomeSuccess
	errText := ""
	if turnErr != nil {
		outcome = traceOutcomeFailure
		errText = turnErr.Error()
	}
	steps := make([]traceStepMirror, len(traj.Steps))
	for i, s := range traj.Steps {
		steps[i] = traceStepMirror{
			Action:  s.Action,
			Input:   s.Input,
			Output:  s.Output,
			Success: s.Success && turnErr == nil,
		}
	}
	return &traceRecordMirror{
		ID:             pkgid.Generate("trace-"),
		SessionID:      sessionID,
		Domain:         traj.Domain,
		Outcome:        outcome,
		Error:          errText,
		InjectedSkills: injectedSkills,
		Steps:          steps,
		Summary:        summary,
		CreatedAt:      time.Now().UTC(),
	}
}

// writeTraceHook persists one turn's trace through the loop's TraceWriter.
// It runs on background goroutines only; write errors are logged at Debug and
// never affect the turn. Safe to call with a nil receiver writer (no-op).
func (l *AgentLoop) writeTraceHook(traj Trajectory, sessionID string, injectedSkills []string, turnErr error, summary string) {
	l.mu.RLock()
	tw := l.traceWriter
	l.mu.RUnlock()
	if tw == nil {
		return
	}
	rec := buildTraceRecord(traj, sessionID, injectedSkills, turnErr, summary)
	if _, err := tw.WriteTrace(rec); err != nil {
		logger := l.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Debug("trace write failed", "session", sessionID, "error", err)
	}
}

// tryWriteTrace launches writeTraceHook on a wg-tracked background goroutine
// if the loop has a trace writer and a conversation snapshot is available.
// turnErr may be nil (success turn) or non-nil (failure turn); the trace is
// written either way. This runs BEFORE the learning pipeline's judge path so
// evidence is durably recorded even if the judge call fails or the process
// dies mid-judgment.
func (l *AgentLoop) tryWriteTrace(traj Trajectory, sessionID string, injectedSkills []string, turnErr error, summary string) {
	l.mu.RLock()
	hasWriter := l.traceWriter != nil
	l.mu.RUnlock()
	if !hasWriter {
		return
	}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				logger := l.logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn("trace write panicked", "session", sessionID, "error", r)
			}
		}()
		l.writeTraceHook(traj, sessionID, injectedSkills, turnErr, summary)
	}()
}
