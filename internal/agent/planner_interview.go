package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// Interview pause surfacing + answer seam (interactive-usability leaf 04,
// master contract C-3): the planner's design-interview pause becomes
// visible and answerable through the ordinary chat surfaces.
//
// Persisted shape (frozen; consumers may rely on the key): a
// session_messages row with role=assistant, content = the formatted
// questions, and metadata.planner_interview = task_id. The next user
// message in the same conversation is matched to the waiting planner and
// delivered as answers; the planner resumes decomposition through the
// EXISTING interview machinery (PlanningCtx.InterviewAnswers ->
// InterviewCompleted). No new bus topic; no WS schema change.
//
// State shape: the task's planning-state metadata gains a
// planner_interview block while the interview waits. TurnStateFunc
// (internal/agent/turn_watchdog.go C7 exemption) reads it so a waiting
// interview is exempt from staleness reaping; the handler's answer
// lookup reads the same block instead of scanning session messages.

// PlannerInterviewMetaKey is the session-message metadata key that marks
// an assistant message as a planner interview pause. The value is the
// owning task id.
const PlannerInterviewMetaKey = "planner_interview"

// interviewWaitWindow bounds how long a parked interview stays exempt
// from watchdog reaping before it degrades to a normal stale turn. There
// is deliberately NO config knob yet: the interview is a human-in-the
// loop pause and a long wait is correct behavior; the window only bounds
// an orphaned interview nobody will ever answer.
const interviewWaitWindow = 24 * time.Hour

// plannerInterviewState is the task-metadata block written at the pause
// point and cleared on answer delivery. It is the single source of truth
// for "is this task awaiting interview answers" — both the handler's
// answer seam and the watchdog's exemption consult it, so they can never
// disagree about which tasks wait.
type plannerInterviewState struct {
	TaskID     string   `json:"task_id"`
	SessionID  string   `json:"session_id,omitempty"`
	Questions  []string `json:"questions"`
	SentAt     string   `json:"sent_at"`
	AnsweredBy string   `json:"answered_by,omitempty"`
}

// InterviewMessageSink is the session-persistence lane the planner uses
// to publish interview questions as chat-visible history: SaveMessages
// semantics over one assistant entry. Production wires the session store
// through SetInterviewMessageSink; tests capture the message.
type InterviewMessageSink func(sessionID string, msg session.Message) error

// SetInterviewMessageSink wires the session-store lane for interview
// question persistence. Nil-guarded per the setter convention; nil
// disables persistence (the pause still parks and answers still resume).
func (sp *StrategicPlanner) SetInterviewMessageSink(sink InterviewMessageSink) {
	if sp == nil {
		return
	}
	sp.mu.Lock()
	sp.interviewMessageSink = sink
	sp.mu.Unlock()
}

// interviewSink reads the sink nil-safely under the planner mutex.
func (sp *StrategicPlanner) interviewSink() InterviewMessageSink {
	if sp == nil {
		return nil
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.interviewMessageSink
}

// formatInterviewQuestions renders the questions as user-facing chat
// text. Numbered, plain (ASCII only per repo convention), with the
// reply-to-continue instruction so the surface teaches the answer path.
func formatInterviewQuestions(questions []string) string {
	var b strings.Builder
	b.WriteString("I need a few answers before I can plan this task:\n")
	for i, q := range questions {
		fmt.Fprintf(&b, "\n%d. %s", i+1, q)
	}
	b.WriteString("\n\nReply to continue with your answers.")
	return b.String()
}

// StoreInterviewState writes the waiting-interview block into the task's
// metadata and stamps the waiting-since time. Best-effort: persistence
// failures degrade to the in-memory waiter (logged, never fatal — the
// pause itself already happened).
func (sp *StrategicPlanner) StoreInterviewState(t *task.Task, req PlanRequest, st *plannerInterviewState) {
	st.SentAt = time.Now().UTC().Format(time.RFC3339)
	if b, err := json.Marshal(st); err == nil && sp.taskStore != nil {
		meta := map[string]any{}
		if len(t.Metadata) > 0 {
			if err := json.Unmarshal(t.Metadata, &meta); err != nil {
				meta = map[string]any{}
			}
		}
		meta["planner_interview"] = json.RawMessage(b)
		if metaJSON, mErr := json.Marshal(meta); mErr == nil {
			prev := t.Metadata
			t.Metadata = json.RawMessage(metaJSON)
			if err := sp.taskStore.Update(t); err != nil {
				t.Metadata = prev
				sp.logger.Warn("Failed to persist interview wait state",
					"task_id", req.TaskID, "error", err)
			}
		}
	}
}

// ClearInterviewState removes the waiting block after answers arrive.
func (sp *StrategicPlanner) ClearInterviewState(taskID string) {
	if sp == nil || sp.taskStore == nil || taskID == "" {
		return
	}
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return
	}
	meta := map[string]any{}
	if len(t.Metadata) > 0 {
		if err := json.Unmarshal(t.Metadata, &meta); err != nil {
			return
		}
	}
	if _, ok := meta["planner_interview"]; !ok {
		return
	}
	delete(meta, "planner_interview")
	if metaJSON, mErr := json.Marshal(meta); mErr == nil {
		prev := t.Metadata
		t.Metadata = json.RawMessage(metaJSON)
		if err := sp.taskStore.Update(t); err != nil {
			t.Metadata = prev
			sp.logger.Warn("Failed to clear interview wait state",
				"task_id", taskID, "error", err)
		}
	}
}

// ReadInterviewState reads the waiting-interview block from a task's
// persisted metadata. Returns nil when the task carries none (or cannot
// be read) — callers treat nil as "no interview waits".
func ReadInterviewState(t *task.Task) *plannerInterviewState {
	if t == nil || len(t.Metadata) == 0 {
		return nil
	}
	meta := map[string]any{}
	if err := json.Unmarshal(t.Metadata, &meta); err != nil {
		return nil
	}
	raw, ok := meta["planner_interview"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case string:
		var st plannerInterviewState
		if err := json.Unmarshal([]byte(v), &st); err != nil || st.TaskID == "" {
			return nil
		}
		return &st
	case json.RawMessage:
		var st plannerInterviewState
		if err := json.Unmarshal(v, &st); err != nil || st.TaskID == "" {
			return nil
		}
		return &st
	default:
		return nil
	}
}

// InterviewWaitingSince reports when the task's interview was sent (zero
// when no interview waits). Exported for the watchdog exemption's age
// bound.
func InterviewWaitingSince(t *task.Task) time.Time {
	st := ReadInterviewState(t)
	if st == nil {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, st.SentAt)
	if err != nil {
		return time.Time{}
	}
	return at
}

// interviewWaiter is one parked interview: the resume path plus the
// conversation it is parked on.
type interviewWaiter struct {
	resume    func(answers []string)
	sessionID string
	sentAt    time.Time
}

// beginInterviewWait parks the planning task on an in-memory waiter and
// spawns nothing: DeliverInterviewAnswer drives the resume goroutine.
// taskID keys the map; sessionID keys the handler lookup.
func (sp *StrategicPlanner) beginInterviewWait(taskID, sessionID string, resume func([]string)) {
	if sp == nil {
		return
	}
	sp.mu.Lock()
	if sp.interviewWaiters == nil {
		sp.interviewWaiters = make(map[string]*interviewWaiter)
	}
	sp.interviewWaiters[taskID] = &interviewWaiter{
		resume:    resume,
		sessionID: sessionID,
		sentAt:    time.Now(),
	}
	sp.mu.Unlock()

	// Watchdog liveness (leaf 04): touch the tracked turn at the pause
	// point AND on a slow ticker for as long as the interview waits, so
	// a healthy human think-time never reads as staleness. The ticker
	// exits when the waiter is resolved or the wait window elapses (an
	// orphaned interview then ages into normal reap semantics).
	if sp.turnLivenessTouch != nil {
		sp.touchTurnLiveness(taskID)
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		deadline := time.Now().Add(interviewWaitWindow)
		for {
			select {
			case <-ticker.C:
			}
			if !sp.interviewWaitIsLive(taskID) {
				return
			}
			if time.Now().After(deadline) {
				return
			}
			sp.touchTurnLiveness(taskID)
		}
	}()
}

// interviewWaitIsLive reports whether taskID still has an unresolved
// waiter.
func (sp *StrategicPlanner) interviewWaitIsLive(taskID string) bool {
	if sp == nil {
		return false
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	_, ok := sp.interviewWaiters[taskID]
	return ok
}

// DeliverInterviewAnswer routes one user message to the interview parked
// on sessionID's conversation. Returns (ack, true) when delivered (and
// the waiter consumed — duplicate answers are refused by construction),
// ("", false) when no interview waits for that conversation.
func (sp *StrategicPlanner) DeliverInterviewAnswer(sessionID, answer string) (string, bool) {
	if sp == nil || sessionID == "" {
		return "", false
	}
	sp.mu.Lock()
	var taskID string
	var waiter *interviewWaiter
	for tid, w := range sp.interviewWaiters {
		if w.sessionID == sessionID {
			taskID, waiter = tid, w
			break
		}
	}
	if waiter == nil {
		sp.mu.Unlock()
		return "", false
	}
	delete(sp.interviewWaiters, taskID)
	sp.mu.Unlock()

	sp.ClearInterviewState(taskID)

	ack := "Thanks — resuming the plan with your answers."
	go waiter.resume([]string{answer})
	return ack, true
}

// reqAmbiguities recovers the original ambiguity list for the resume
// PlanningContext: the prior planning context's ambiguities, else the
// TrueAnalysis suggestions, else the questions themselves.
func reqAmbiguities(req PlanRequest) []string {
	if req.PlanningCtx != nil && len(req.PlanningCtx.Ambiguities) > 0 {
		return req.PlanningCtx.Ambiguities
	}
	if req.TrueAnalysis != nil && len(req.TrueAnalysis.SuggestedQuestions) > 0 {
		return req.TrueAnalysis.SuggestedQuestions
	}
	return nil
}

// resumeInterview resumes a parked interview with the delivered answers:
// it re-invokes the planner's Plan through the EXISTING interview
// machinery — PlanningCtx.InterviewAnswers marks the interview completed
// (ConductInterview) and decomposition proceeds with the verified
// context. Async: the answer path must not block the chat turn.
func (sp *StrategicPlanner) resumeInterview(taskID string, req PlanRequest, questions []string) func([]string) {
	return func(answers []string) {
		if sp.taskStore != nil {
			if t, err := sp.taskStore.GetByID(taskID); err == nil && t != nil {
				t.SetState(task.StatePlanning)
				if err := sp.taskStore.Update(t); err != nil {
					sp.logger.Warn("interview resume: task state re-stamp failed", "task_id", taskID, "error", err)
				}
			}
		}
		// Split one free-text reply across the question count: a single
		// reply answering N questions lands as N entries so the resume
		// prompt preserves question↔answer pairing.
		split := splitInterviewAnswer(answers, len(questions))
		resumeReq := req
		resumeReq.PlanningCtx = &plan.PlanningContext{
			InterviewQuestions: questions,
			InterviewAnswers:   split,
			InterviewCompleted: true,
			Ambiguities:        reqAmbiguities(req),
		}
		resumeReq.SessionContext = ""
		if err := sp.Plan(context.Background(), resumeReq); err != nil {
			sp.logger.Error("Interview resume planning failed",
				"task_id", taskID, "error", err)
			sp.failTaskWithReason(taskID, "planning failed after interview answers: "+err.Error())
			return
		}
		sp.logger.Info("Interview resume planning succeeded", "task_id", taskID)
	}
}

// splitInterviewAnswer distributes a free-text reply over the question
// count: one reply line per question when the counts line up, otherwise
// the whole text rides as the single first answer (the resume prompt
// carries both slices, so the pairing survives either way).
func splitInterviewAnswer(answers []string, questionCount int) []string {
	joined := strings.Join(answers, "\n")
	if len(answers) >= questionCount && questionCount > 0 {
		return answers
	}
	if questionCount <= 1 {
		return []string{joined}
	}
	// Non-empty lines as answers when the user replied with several.
	var lines []string
	for _, a := range answers {
		for _, line := range strings.Split(a, "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	if len(lines) >= questionCount {
		return lines[:questionCount]
	}
	return append(lines, joined)
}

// deliverPlannerInterview is the pause-point entry called from
// awaitInterviewAnswers: persists the questions as a chat-visible
// session message, stamps the task metadata, parks the answer waiter,
// and Touches the tracked turn so the watchdog sees a live human wait.
// Best-effort persistence: a missing sink (unit tests, CLI evals) or a
// store failure never fails the pause itself.
func (sp *StrategicPlanner) deliverPlannerInterview(t *task.Task, req PlanRequest, pctx *plan.PlanningContext) {
	questions := pctx.InterviewQuestions
	if len(questions) == 0 {
		return
	}

	// 1. Persist the questions as an assistant session message (same
	// lane as formatTaskCompletedMessage) with metadata
	// planner_interview = task_id. Send on session id first, then the
	// conversation id: session_messages keys on the session row id.
	if sink := sp.interviewSink(); sink != nil {
		msg := session.Message{
			SessionID: req.SessionID,
			Role:      "assistant",
			Content:   formatInterviewQuestions(questions),
			Timestamp: time.Now().UTC(),
			EntryType: "message",
			BranchID:  "main",
			AgentID:   "orchestrator",
			Metadata: map[string]string{
				PlannerInterviewMetaKey: req.TaskID,
			},
		}
		if err := sink(req.SessionID, msg); err != nil {
			sp.logger.Warn("Failed to persist interview questions as session message",
				"task_id", req.TaskID, "session_id", req.SessionID, "error", err)
		}
	}

	// 2. Stamp the task's waiting-interview state (watchdog exemption +
	// handler answer lookup).
	sp.StoreInterviewState(t, req, &plannerInterviewState{
		TaskID:    req.TaskID,
		SessionID: req.SessionID,
		Questions: questions,
	})

	// 3. Park the answer waiter; resume re-enters Plan with the answers.
	sp.beginInterviewWait(req.TaskID, req.SessionID, sp.resumeInterview(req.TaskID, req, questions))

	sp.recordMetric("strategic_planner.interview_persisted", 1, map[string]string{
		"intent": req.Intent, "mode": req.Mode,
	})
}

// HasPendingInterview reports whether any planner interview currently
// waits for answers in the given conversation.
func (sp *StrategicPlanner) HasPendingInterview(sessionID string) bool {
	if sp == nil || sessionID == "" {
		return false
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	for _, w := range sp.interviewWaiters {
		if w.sessionID == sessionID {
			return true
		}
	}
	return false
}

// handleTaskInterview consumes the planner's task.interview pause event
// and terminalizes the SUBMITTED turn with the questions text: status
// completed, handler_case interview_pause. A one-shot CLI waiting on
// turn.terminal gets the questions as the turn result and exits 0 (leaf
// task 4, one-shot honesty) instead of waiting out its liveness window
// and dying with "turn stalled". The registry edge survives: the
// task->turn link stays so the later answer path can still correlate.
// Untracked (legacy) turns have no registry row and no-op.
func (h *ChatHandler) handleTaskInterview(msg *models.BusMessage) {
	var payload struct {
		TaskID    string   `json:"task_id"`
		SessionID string   `json:"session_id"`
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.TaskID == "" {
		h.logger.Debug("task.interview payload unparsable; skipped",
			"error", err)
		return
	}
	if len(payload.Questions) == 0 {
		return
	}
	turnID := h.turnIDForTask(payload.TaskID)
	if turnID == "" {
		// Headless task or legacy untracked turn: nothing to terminalize;
		// the questions still reached the session history via the sink.
		return
	}
	// Attach the task id so the terminal event carries it (reap events
	// do the same), then emit WITHOUT completing the registry row.
	h.attachTask(turnID, payload.TaskID)
	h.publishTurnTerminal(TurnTerminalEvent{
		ConversationID: payload.SessionID,
		TurnID:         turnID,
		TaskID:         payload.TaskID,
		HandlerCase:    "interview_pause",
		Status:         "completed",
		Reply:          formatInterviewQuestions(payload.Questions),
	})
	h.logger.Info("Interview pause terminalized submitted turn with questions",
		"task_id", payload.TaskID,
		"turn_id", turnID,
		"question_count", len(payload.Questions),
	)
}

// TurnStateFunc adapts a task store into the watchdog's C7 exemption
// accessor, extended for the interview pause: planning AND any task
// carrying a fresh waiting-interview block are exempt (a human wait is
// not a stalled turn). A nil store keeps the handler-less nil behavior.
// Pure store read per call — the watchdog calls it only for turns that
// are already stale, so the per-tick cost is bounded by the stale set.
func TurnStateFunc(store *task.Store) func(taskID string) task.TaskState {
	if store == nil {
		return nil
	}
	return func(taskID string) task.TaskState {
		t, err := store.GetByID(taskID)
		if err != nil || t == nil {
			return ""
		}
		if InterviewWaitingSince(t) != (time.Time{}) {
			if sentAt := InterviewWaitingSince(t); time.Since(sentAt) < interviewWaitWindow {
				return "awaiting_interview"
			}
		}
		return t.State
	}
}
