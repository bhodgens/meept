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
	// The ACP subprocess is a LONG-LIVED daemon child: it must not die
	// with the turn-scoped context that first launched it
	// (acp-human-permission-relay leaf 03 full-stack e2e: the launch
	// turn's RPC ctx cancel killed the agent between turns and left a
	// closed session in the registry — "send: closed"). Detach the
	// process lifetime from the caller; the handshake/dial wait below
	// still honors the caller ctx so a canceled GetOrCreate never hangs.
	procCtx := context.WithoutCancel(ctx)
	cmd := exec.CommandContext(procCtx, name, args...)
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
	// Written under s.mu: the Serve goroutine (launched above) reads the
	// session id in relayPermissionWait, and an agent may emit
	// session/requestPermission before session/new replies land here.
	s.setSessionID(newRes.SessionID)
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

// setSessionID records the session id under s.mu. Start launches the
// Serve goroutine before the session/new reply lands, and the permission
// lanes (relayPermissionWait) read the id from that goroutine — an
// unguarded write races an early session/requestPermission.
func (s *Session) setSessionID(id string) {
	s.mu.Lock()
	s.sessionID = id
	s.mu.Unlock()
}

// getSessionID returns the session id under s.mu (see setSessionID).
func (s *Session) getSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
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
		"sessionId": s.getSessionID(),
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
		"sessionId": s.getSessionID(),
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

// defaultAllowOptionID is the fallback optionId for allow replies when the
// agent offered no options (or none that looks like allow-once). It is the
// id the v1 lanes hardcoded; keeping it preserves today's behavior for
// agents that don't send an options list.
const defaultAllowOptionID = "allow-once"

// PermissionOption is one entry of the ACP requestPermission params'
// options array (optionId + name + kind). Exported for the relay snapshot
// (PendingPermission.Options) and the daemon notifier's bus payload —
// internal/bus mirrors the {optionId,name,kind} wire tags rather than
// importing this package (no import cycle).
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// parsePermissionOptions decodes the requestPermission params' options
// array. nil-safe on absent/malformed params (the lane never errors on a
// weird agent payload — the reply chain falls back).
func parsePermissionOptions(rawParams []byte) []PermissionOption {
	if len(rawParams) == 0 {
		return nil
	}
	var p struct {
		Options []PermissionOption `json:"options"`
	}
	if err := json.Unmarshal(rawParams, &p); err != nil || len(p.Options) == 0 {
		return nil
	}
	return p.Options
}

// resolveAlwaysOptionID picks the optionId of the agent's own allow_always
// option (acp-always-allow master.md Contract A, design decision 1: meept
// is a TRANSPORT for "always" — the reply echoes the agent's id and the
// AGENT persists the grant). Matching: kind == "allow_always" first (the
// ACP spec token), then a kind containing BOTH "allow" and "always"
// (case-insensitive); an option with an empty id never matches. Returns ""
// when no option looks like always — the caller then falls back to the
// resolveAllowOptionID chain, so "always" on an agent that offers no
// allow_always option resolves exactly like "allow" (never errors, never
// silently denies).
func resolveAlwaysOptionID(rawParams []byte) string {
	options := parsePermissionOptions(rawParams)
	for _, o := range options {
		if o.OptionID != "" && o.Kind == "allow_always" {
			return o.OptionID
		}
	}
	for _, o := range options {
		kind := strings.ToLower(o.Kind)
		if o.OptionID != "" && strings.Contains(kind, "allow") && strings.Contains(kind, "always") {
			return o.OptionID
		}
	}
	return ""
}

// resolveAllowOptionID picks the optionId of the allow-once option the
// agent actually offered, falling back to defaultAllowOptionID when no
// options are present or none matches. ACP agents are free to choose
// their own ids; replying with an id the agent never offered names a
// nonexistent option. Matching: kind "allow_once" first (the ACP spec
// token), then a kind containing "allow", then a name that is exactly
// (case-insensitive) one of the allow-once phrasings.
func resolveAllowOptionID(rawParams []byte) string {
	options := parsePermissionOptions(rawParams)
	if options == nil {
		return defaultAllowOptionID
	}
	fallback := defaultAllowOptionID
	for _, o := range options {
		if o.OptionID == "" {
			continue
		}
		switch {
		case o.Kind == "allow_once":
			return o.OptionID
		case fallback == defaultAllowOptionID &&
			strings.Contains(strings.ToLower(o.Kind), "allow"):
			fallback = o.OptionID
		}
	}
	if fallback != defaultAllowOptionID {
		return fallback
	}
	for _, o := range options {
		if o.OptionID == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(o.Name)) {
		case "allow once", "allow-once", "allow", "yes":
			return o.OptionID
		}
	}
	return defaultAllowOptionID
}

// allowOutcomeResult builds the ACP "selected" reply for one permission request,
// naming an optionId the agent actually offered (falling back to the
// legacy "allow-once" literal when nothing matches). Shared by the
// permissive lane, the relay outcome reply, and the decision lane.
func allowOutcomeResult(rawParams []byte) map[string]any {
	return map[string]any{
		"outcome": map[string]any{"outcome": "selected", "optionId": resolveAllowOptionID(rawParams)},
	}
}

// alwaysOutcomeResult builds the "selected" reply for a human "always"
// answer: the agent's OWN allow_always optionId when offered (the agent
// persists the grant — meept transports, it never memorizes), else the
// resolveAllowOptionID chain (allow_once echo -> defaultAllowOptionID), so
// "always" degrades exactly like "allow" for agents without the capability.
func alwaysOutcomeResult(rawParams []byte) map[string]any {
	optionID := resolveAlwaysOptionID(rawParams)
	if optionID == "" {
		optionID = resolveAllowOptionID(rawParams)
	}
	return map[string]any{
		"outcome": map[string]any{"outcome": "selected", "optionId": optionID},
	}
}

// cancelledResult builds the ACP "cancelled" reply (fail-closed deny).
func cancelledResult() map[string]any {
	return map[string]any{
		"outcome": map[string]any{"outcome": "cancelled"},
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
		result = allowOutcomeResult(n.Params)
	} else {
		result = cancelledResult()
	}
	s.emit(SessionEvent{Kind: "permission_request", Text: kind})
	if err := s.tr.Reply(*n.ID, result, nil); err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
	}
}

// handlePermissionHuman is the fail-CLOSED human relay lane (Contract B).
// Like the decision lane it runs on a dedicated permission goroutine
// (Transport.dispatchPermission, head-of-line fix), reads s.cfg (immutable
// after Start) without s.mu, and NEVER holds s.mu across the relay wait
// or the notifier calls (mutexio). Missing seams cancel immediately — the
// pin mirrors TestSession_DecisionNilDecisionerDenies.
func (s *Session) handlePermissionHuman(n Notification) {
	if s.cfg.Relay == nil || s.cfg.Notifier == nil {
		slog.Default().Warn("acp permission denied: human relay not wired",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"reason", relayReasonNoWire,
		)
		s.emit(SessionEvent{Kind: "permission_resolved", Text: relayDecisionDeny})
		s.replyPermissionOutcome(n, relayVerdict{})
		return
	}
	verdict := s.relayPermissionWait(*n.ID, n.Params)
	s.replyPermissionOutcome(n, verdict)
}

// replyPermissionOutcome writes the ACP reply frame for one permission
// request: allow -> selected/<offered allow-once optionId>, always ->
// selected/<offered allow_always optionId (fallback chain above)>,
// otherwise cancelled. The human relay lane (and its escalation twin) is
// the only caller path that can report always — the decision-lane model
// options stay [allow deny] (master.md design decision 4).
func (s *Session) replyPermissionOutcome(n Notification, verdict relayVerdict) {
	var result any
	switch {
	case verdict.always:
		result = alwaysOutcomeResult(n.Params)
	case verdict.allow:
		result = allowOutcomeResult(n.Params)
	default:
		result = cancelledResult()
	}
	if err := s.tr.Reply(*n.ID, result, nil); err != nil {
		s.emit(SessionEvent{Kind: "error", Text: err.Error()})
	}
}

// handlePermissionDecision is the fail-CLOSED decision lane (Contract B).
// Runs on a dedicated permission goroutine (Transport.dispatchPermission,
// head-of-line fix): s.cfg is immutable after Start, so its fields are
// read without s.mu — and s.mu is NEVER held across Ask (mutexio).
// handlePermission has no caller context, so the Ask is bounded by
// decisionAskTimeout off the transport's ServeContext (Close cancels it).
func (s *Session) handlePermissionDecision(n Notification) {
	// Derive the bounded Ask context from the transport lifetime so a
	// Transport Close cancels an in-flight decision wait (head-of-line
	// fix: permission handlers run on their own goroutine and must not
	// outlive the transport).
	base := context.Background()
	if s.tr != nil {
		base = s.tr.ServeContext()
	}
	askCtx, cancel := context.WithTimeout(base, decisionAskTimeout)
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
	result := cancelledResult()
	if outcome.allow {
		result = allowOutcomeResult(n.Params)
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
		s.replyPermissionOutcome(n, relayVerdict{})
		return
	}
	verdict := s.relayPermissionWait(*n.ID, n.Params)
	text := outcome.text
	switch verdict.text {
	case relayDecisionAllow, relayDecisionAlways:
		// A human "always" is still a human ALLOW (scope differs; the
		// reply echo does that) — the decision lane itself can never
		// vote always (design decision 4).
		text = escalatedTextAllow
	case relayDecisionDeny:
		text = escalatedTextDeny
	case relayTextTimeout:
		// Timeout keeps parent cancel semantics: plain "deny" token.
		text = decisionTextDeny
	}
	s.emit(SessionEvent{Kind: "permission_request", Text: text})
	s.emit(SessionEvent{Kind: "permission_decision", Text: text})
	s.replyPermissionOutcome(n, verdict)
}
