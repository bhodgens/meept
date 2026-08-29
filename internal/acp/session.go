package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
		s.closeLocked()
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
		s.closeLocked()
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
	out := ""
	for _, c := range s.chunks {
		out += c
	}
	s.mu.Unlock()
	s.emit(SessionEvent{Kind: "done", Text: out})
	return out, nil
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
	return err == os.ErrProcessDone || err == exec.ErrNotFound
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
