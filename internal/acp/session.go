package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// SessionConfig is the spawn/handshake configuration for one ACP session.
type SessionConfig struct {
	AgentID        string
	Command        []string
	Env            map[string]string
	Cwd            string
	DefaultMode    string
	DialTimeout    time.Duration
	CallTimeout    time.Duration
	PermissionMode string
	// Decision is the permission lane's decision client, set ONLY when
	// PermissionMode == "decision" (zero value = disabled lane).
	Decision Decisioner
	// DecisionThreshold is the confidence bar for the decision lane
	// ([acp] decision_threshold). Zero value = disabled lane.
	DecisionThreshold float64
	// Relay is the human permission relay registry, and Notifier its
	// publish seam (acp-human-permission-relay leaf 02, Contract B).
	// Stamped ONLY for mode "human", or mode "decision" with
	// DecisionEscalateToHuman (zero values = relay lane disabled, lanes
	// fail closed).
	Relay    *Relay
	Notifier PermissionNotifier
	// RelayTimeout is the fail-closed human wait ([acp] relay_timeout,
	// seconds in config -> Duration here). Zero = defaultRelayTimeout.
	RelayTimeout time.Duration
	// DecisionEscalateToHuman routes NON-allow decision outcomes through
	// the relay wait before cancelling ([acp] decision_escalate_to_human).
	DecisionEscalateToHuman bool
}

// SessionState is the lifecycle state of an ACP session.
type SessionState int

const (
	StateStarting SessionState = iota
	StateReady
	StateBusy
	StateClosed
)

// SessionEvent is a projected ACP notification for higher layers.
type SessionEvent struct {
	Kind string
	Text string
}

// Session is one live ACP agent subprocess.
type Session struct {
	cfg       SessionConfig
	cmd       *exec.Cmd
	tr        *Transport
	sessionID string

	mu     sync.Mutex
	state  SessionState
	chunks []string
	events chan SessionEvent
}

// Start spawns the agent subprocess and runs the ACP handshake.
func Start(ctx context.Context, cfg SessionConfig) (*Session, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("acp: start %s: empty command", cfg.AgentID)
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 120 * time.Second
	}

	name := cfg.Command[0]
	args := cfg.Command[1:]
	cmd := exec.CommandContext(ctx, name, args...)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	if len(cfg.Env) > 0 {
		env := os.Environ()
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: start %s stdin: %w", cfg.AgentID, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: start %s stdout: %w", cfg.AgentID, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start %s command %s: %w", cfg.AgentID, name, err)
	}

	s := &Session{
		cfg:    cfg,
		cmd:    cmd,
		tr:     NewTransport(stdout, stdin),
		state:  StateStarting,
		events: make(chan SessionEvent, 32),
	}
	s.tr.OnNotification(s.onNotice)
	go s.tr.Serve()
	go s.reap()

	hctx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()

	var init InitializeResult
	if err := s.tr.Call(hctx, MethodInitialize, InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      ImplementationInfo{Name: "meept", Version: "0.1"},
	}, &init); err != nil {
		// Best-effort teardown during startup failure; the primary error below
		// is what the caller sees.
		_ = s.closeLocked()
		return nil, fmt.Errorf("acp: start %s handshake: %w", cfg.AgentID, err)
	}

	cwd := cfg.Cwd
	if cwd == "" {
		cwd = "/"
	}
	var newRes struct {
		SessionID string `json:"sessionId"`
	}
	if err := s.tr.Call(hctx, MethodSessionNew, map[string]any{
		"cwd": cwd,
	}, &newRes); err != nil {
		// Best-effort teardown during startup failure; the primary error below
		// is what the caller sees.
		_ = s.closeLocked()
		return nil, fmt.Errorf("acp: start %s session/new: %w", cfg.AgentID, err)
	}
	if newRes.SessionID == "" {
		newRes.SessionID = id.Generate("sess")
	}
	s.sessionID = newRes.SessionID
	s.setState(StateReady)
	return s, nil
}

func (s *Session) reap() {
	if s.cmd == nil {
		return
	}
	if err := s.cmd.Wait(); err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
	}
	s.setState(StateClosed)
	s.emit(SessionEvent{Kind: "closed"})
}

// State returns the current session lifecycle state.
func (s *Session) State() SessionState {
	if s == nil {
		return StateClosed
	}
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	return st
}

func (s *Session) setState(st SessionState) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

// Events returns the buffered event stream. Full buffer drops oldest.
func (s *Session) Events() <-chan SessionEvent {
	return s.events
}

func (s *Session) emit(ev SessionEvent) {
	select {
	case s.events <- ev:
	default:
		select {
		case <-s.events:
		default:
		}
		select {
		case s.events <- ev:
		default:
		}
	}
}

// Send submits a user turn and returns the agent's final chat text.
func (s *Session) Send(ctx context.Context, text string) (string, error) {
	if s.State() == StateClosed {
		return "", fmt.Errorf("acp: send %s: closed", s.cfg.AgentID)
	}
	s.setState(StateBusy)
	defer s.setState(StateReady)

	s.mu.Lock()
	s.chunks = nil
	s.mu.Unlock()

	callCtx := ctx
	if s.cfg.CallTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, s.cfg.CallTimeout)
		defer cancel()
	}

	var res struct {
		StopReason string `json:"stopReason"`
	}
	err := s.tr.Call(callCtx, MethodSessionPrompt, map[string]any{
		"sessionId": s.sessionID,
		"prompt":    []map[string]string{{"type": "text", "text": text}},
	}, &res)
	if err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
		return "", fmt.Errorf("acp: send %s: %w", s.cfg.AgentID, err)
	}

	s.mu.Lock()
	var out strings.Builder
	for _, c := range s.chunks {
		out.WriteString(c)
	}
	s.mu.Unlock()
	s.emit(SessionEvent{Kind: "done", Text: out.String()})
	return out.String(), nil
}

// Cancel interrupts the in-flight turn.
func (s *Session) Cancel() error {
	if s.tr == nil {
		return nil
	}
	if err := s.tr.Notify(MethodSessionCancel, map[string]any{
		"sessionId": s.sessionID,
	}); err != nil {
		return fmt.Errorf("acp: cancel %s: %w", s.cfg.AgentID, err)
	}
	return nil
}

// Close kills the subprocess tree. Idempotent.
func (s *Session) Close() error {
	return s.closeLocked()
}

func (s *Session) closeLocked() error {
	s.mu.Lock()
	if s.state == StateClosed {
		s.mu.Unlock()
		return nil
	}
	s.state = StateClosed
	cmd := s.cmd
	tr := s.tr
	s.mu.Unlock()

	if tr != nil {
		if closeErr := tr.Close(); closeErr != nil && cmd == nil {
			return fmt.Errorf("acp: close %s transport: %w", s.cfg.AgentID, closeErr)
		}
	}
	if cmd != nil && cmd.Process != nil {
		pgid := cmd.Process.Pid
		if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
			if killErr := cmd.Process.Kill(); killErr != nil && !isAlreadyDone(killErr) {
				return fmt.Errorf("acp: close %s: %w", s.cfg.AgentID, killErr)
			}
		}
	}
	return nil
}

func isAlreadyDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, exec.ErrNotFound)
}

func (s *Session) onNotice(n Notification) {
	if n.Method == MethodSessionRequestPermission && n.ID != nil {
		s.handlePermission(n)
		return
	}
	if n.Method != MethodSessionUpdate {
		return
	}
	var p struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Title string `json:"title"`
		} `json:"update"`
	}
	if err := json.Unmarshal(n.Params, &p); err != nil {
		return
	}
	switch p.Update.SessionUpdate {
	case "agent_message_chunk":
		s.mu.Lock()
		s.chunks = append(s.chunks, p.Update.Content.Text)
		s.mu.Unlock()
		s.emit(SessionEvent{Kind: "message_chunk", Text: p.Update.Content.Text})
	case "tool_call", "tool_call_update":
		s.emit(SessionEvent{Kind: "tool_call", Text: p.Update.Title})
	}
}

func (s *Session) handlePermission(n Notification) {
	mode := s.cfg.PermissionMode
	if mode == "decision" {
		s.handlePermissionDecision(n)
		return
	}
	if mode == "human" {
		s.handlePermissionHuman(n)
		return
	}
	// permissive / deny lanes: byte-identical pre-decision behavior, no new
	// parses on these paths (Contract B).
	approved := mode == "permissive"
	kind := "denied"
	var result any
	if approved {
		kind = "approved"
		result = map[string]any{
			"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"},
		}
	} else {
		result = map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		}
	}
	s.emit(SessionEvent{Kind: "permission_request", Text: kind})
	if err := s.tr.Reply(*n.ID, result, nil); err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
	}
}

// handlePermissionHuman is the fail-CLOSED human relay lane (Contract B).
// Like the decision lane it runs on the Transport Serve goroutine, reads
// s.cfg (immutable after Start) without s.mu, and NEVER holds s.mu across
// the relay wait or the notifier calls (mutexio). Missing seams cancel
// immediately — the pin mirrors TestSession_DecisionNilDecisionerDenies.
func (s *Session) handlePermissionHuman(n Notification) {
	if s.cfg.Relay == nil || s.cfg.Notifier == nil {
		slog.Default().Warn("acp permission denied: human relay not wired",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"reason", relayReasonNoWire,
		)
		s.emit(SessionEvent{Kind: "permission_resolved", Text: relayDecisionDeny})
		s.replyPermissionOutcome(n, false)
		return
	}
	verdict := s.relayPermissionWait(*n.ID, n.Params)
	s.replyPermissionOutcome(n, verdict.allow)
}

// replyPermissionOutcome writes the ACP reply frame for one permission
// request: allow -> selected/allow-once, otherwise cancelled (v1 has no
// "always" verb; frozen by master.md user decisions).
func (s *Session) replyPermissionOutcome(n Notification, allow bool) {
	var result any
	if allow {
		result = map[string]any{
			"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"},
		}
	} else {
		result = map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		}
	}
	if err := s.tr.Reply(*n.ID, result, nil); err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
	}
}

// handlePermissionDecision is the fail-CLOSED decision lane (Contract B).
// Runs on the Transport Serve goroutine: s.cfg is immutable after Start, so
// its fields are read without s.mu — and s.mu is NEVER held across Ask
// (mutexio). handlePermission has no caller context, so the Ask is bounded
// by decisionAskTimeout off context.Background().
func (s *Session) handlePermissionDecision(n Notification) {
	askCtx, cancel := context.WithTimeout(context.Background(), decisionAskTimeout)
	defer cancel()
	outcome := decidePermission(askCtx, s.cfg.Decision, s.cfg.DecisionThreshold,
		renderDecisionState(s.cfg.AgentID, n.Params))
	switch outcome.text {
	case decisionTextNoDecisioner, decisionTextUnavailable:
		slog.Default().Warn("acp permission denied: decision layer unavailable",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"reason", outcome.text,
		)
	case decisionTextAskError:
		slog.Default().Warn("acp permission denied: decision ask failed",
			"component", "acp",
			"agent", s.cfg.AgentID,
		)
	case decisionTextDeny, decisionTextAnswerDeny, decisionTextLowConfidence:
		slog.Default().Info("acp permission denied by decision",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"reason", outcome.text,
		)
	}
	// Decision-lane escalation (master.md Contract B): ONLY a non-allow
	// outcome with the relay seam fully wired routes to the human wait;
	// an allow outcome never touches the relay, and escalate=false keeps
	// the parent lane byte-identical.
	if !outcome.allow && s.cfg.DecisionEscalateToHuman {
		s.handleDecisionEscalation(n, outcome)
		return
	}
	result := map[string]any{
		"outcome": map[string]any{"outcome": "cancelled"},
	}
	if outcome.allow {
		result = map[string]any{
			"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"},
		}
	}
	// The permission_request event stays; on this lane only it carries the
	// decision text instead of the hardcoded approved/denied (Contract B).
	s.emit(SessionEvent{Kind: "permission_request", Text: outcome.text})
	s.emit(SessionEvent{Kind: "permission_decision", Text: outcome.text})
	if err := s.tr.Reply(*n.ID, result, nil); err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
	}
}

// handleDecisionEscalation routes a NON-allow decision outcome to the
// human relay before cancelling (drift 6): relay allow ->
// escalated_human_allow + selected/allow-once; relay deny ->
// escalated_human_deny + cancelled; relay timeout -> cancelled with the
// parent's "deny" semantics. Escalate armed but the seam missing is a
// plain cancel with a WARN — fail-closed polarity preserved.
func (s *Session) handleDecisionEscalation(n Notification, outcome decisionOutcome) {
	if s.cfg.Relay == nil || s.cfg.Notifier == nil {
		slog.Default().Warn("acp decision escalation unavailable: human relay not wired",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"reason", escalatedTextNoWire,
		)
		s.emit(SessionEvent{Kind: "permission_request", Text: outcome.text})
		s.emit(SessionEvent{Kind: "permission_decision", Text: outcome.text})
		s.replyPermissionOutcome(n, false)
		return
	}
	verdict := s.relayPermissionWait(*n.ID, n.Params)
	text := outcome.text
	switch verdict.text {
	case relayDecisionAllow:
		text = escalatedTextAllow
	case relayDecisionDeny:
		text = escalatedTextDeny
	case relayTextTimeout:
		// Timeout keeps parent cancel semantics: plain "deny" token.
		text = decisionTextDeny
	}
	s.emit(SessionEvent{Kind: "permission_request", Text: text})
	s.emit(SessionEvent{Kind: "permission_decision", Text: text})
	s.replyPermissionOutcome(n, verdict.allow)
}
