package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/components"
)

// ACPPermissionModal is the human-permission relay surface
// (acp-human-permission-relay leaf 04, Contract D): a centered modal that
// shows the FIFO head of pending acp.permission.requested frames and lets
// the user answer allow/deny over RPC, or dismiss without answering.
//
// Key vocabulary — deliberately the SAME y/n/esc keys as the destructive-
// tool ConfirmationModel precedent (tools.go / confirmation.go), per the
// leaf's "reuse modal accept/decline keys" instruction; no new global
// keybinding is introduced, so gui_inventory.json + parity_test.go are
// untouched. `a` (acp-always-allow leaf 02, Contract B) is a third ANSWER
// key scoped to this modal only:
//
//	y    allow  -> RPC acp.permissions.resolve {id, decision: "allow"}
//	a    always -> RPC acp.permissions.resolve {id, decision: "always"}
//	n    deny   -> RPC acp.permissions.resolve {id, decision: "deny"}
//	esc  dismiss WITHOUT answering; the request stays pending in the queue
//	     (daemon relay_timeout fails it closed) and the status bar shows
//	     "acp perms: N pending" until the next frame or answer.
//
// The footer advertises `a` ONLY when the FIFO head carries an
// allow_always-kind agent option (agent-declared capability; graceful 3-key
// footer otherwise). Pressing `a` on a head without the option is still a
// valid "always" answer — the daemon resolves it exactly like "allow"
// (acp-always-allow master.md design decision 2), never an error.
//
// Once y/n is pressed the head is marked answerDispatched: further answer
// keys no-op until the acp.permission.resolved frame lands (or the resolve
// RPC fails, which RESETS the flag so the key press can retry — contract D
// item 5).
type ACPPermissionModal struct {
	styles *Styles
	queue  []acpPendingRequest
	width  int

	// dispatchCount/lastDispatchID are test seams for the answer→RPC
	// dispatch contract (same-package access; modal_flows_test.go asserts
	// them directly).
	dispatchCount  int
	lastDispatchID string
}

// acpPendingRequest is one queued relay request (decoded from the frozen
// bus.ACPPermissionEvent wire keys: id, agent_id, session_id, summary,
// created_at, plus the additive options key from acp-always-allow leaf 01).
type acpPendingRequest struct {
	ID        string
	AgentID   string
	SessionID string
	Summary   string
	CreatedAt string // RFC3339 as published; rendered as-is

	// Options are the agent-offered permission options (raw labels,
	// {optionId,name,kind}); nil when the frame carried none (old wire or
	// an agent with no options array).
	Options []acpPermissionOption

	// answerDispatched guards double y/n presses after an answer was sent
	// but before the resolved frame confirms it. A failed resolve RPC
	// resets it (retry allowed).
	answerDispatched bool
}

// acpPermissionOption is the TUI decode of one bus
// bus.ACPPermissionOption wire entry ({optionId,name,kind}).
type acpPermissionOption struct {
	OptionID string
	Name     string
	Kind     string
}

// headOffersAlways reports whether the FIFO head carries an allow_always-
// kind option — the agent-declared capability that gates the footer's `a`
// hint. Matching mirrors internal/acp resolveAlwaysOptionID (the read
// side of the same contract): kind == "allow_always" first (the ACP spec
// token), then a kind containing BOTH "allow" and "always"
// (case-insensitive). An option with an empty kind never matches.
func headOffersAlways(opts []acpPermissionOption) bool {
	for _, o := range opts {
		if o.Kind == "allow_always" {
			return true
		}
	}
	for _, o := range opts {
		kind := strings.ToLower(o.Kind)
		if strings.Contains(kind, "allow") && strings.Contains(kind, "always") {
			return true
		}
	}
	return false
}

// decodeACPPermissionOptions turns the payload "options" key (a JSON array
// of {optionId,name,kind} maps as it arrives through the map[string]any
// decode) into the typed slice. Absent/malformed entries are skipped —
// display polish never fails the modal (a head with undecodable options
// just shows the 3-key footer).
func decodeACPPermissionOptions(raw any) []acpPermissionOption {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil
	}
	out := make([]acpPermissionOption, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		opt := acpPermissionOption{}
		opt.OptionID, _ = m["optionId"].(string)
		opt.Name, _ = m["name"].(string)
		opt.Kind, _ = m["kind"].(string)
		if opt.OptionID == "" && opt.Name == "" && opt.Kind == "" {
			continue
		}
		out = append(out, opt)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NewACPPermissionModal constructs the relay modal.
func NewACPPermissionModal(styles *Styles) *ACPPermissionModal {
	return &ACPPermissionModal{styles: styles, width: 60}
}

// Enqueue appends a request to the FIFO queue, ignoring duplicates by ID
// (the relay can re-publish on reconnect races; the modal must never show
// one request twice).
func (m *ACPPermissionModal) Enqueue(r acpPendingRequest) {
	if r.ID == "" {
		return
	}
	for _, q := range m.queue {
		if q.ID == r.ID {
			return
		}
	}
	m.queue = append(m.queue, r)
}

// Remove drops the request with the given id; reports whether it was in
// the queue.
func (m *ACPPermissionModal) Remove(id string) bool {
	for i, q := range m.queue {
		if q.ID == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return true
		}
	}
	return false
}

// Count reports the pending queue length.
func (m *ACPPermissionModal) Count() int { return len(m.queue) }

// PendingLine is the lowercase pending-count line shared by the modal
// footer and the status bar (Contract D item 1).
func (m *ACPPermissionModal) PendingLine() string {
	return fmt.Sprintf("acp perms: %d pending", len(m.queue))
}

// acpPermPendingLine renders the status-bar token text without requiring
// the modal to exist (falls back to the raw count).
func (a *App) acpPermPendingLine() string {
	if a.acpPermissionModal != nil {
		return a.acpPermissionModal.PendingLine()
	}
	return fmt.Sprintf("acp perms: %d pending", a.acpPermPending)
}

// HandleKey processes a key for the modal. Returns:
//
//	("allow"|"always"|"deny", id) — the user answered the FIFO head; id is
//	                      the target request (the flag is marked here so the
//	                      App can dispatch the resolve RPC).
//	("dismiss", "")     — esc: close the modal, queue preserved.
//	("", "")            — key consumed without state change (repeat
//	                      press after dispatch, or empty queue).
func (m *ACPPermissionModal) HandleKey(key string) (action, id string) {
	head := m.head()
	switch key {
	case "esc":
		return "dismiss", ""
	case "y", "Y", "a", "A", "n", "N":
		if head == nil || head.answerDispatched {
			return "", ""
		}
		head.answerDispatched = true
		m.dispatchCount++
		m.lastDispatchID = head.ID
		switch key {
		case "y", "Y":
			return "allow", head.ID
		case "a", "A":
			return "always", head.ID
		}
		return "deny", head.ID
	}
	return "", ""
}

// ResetDispatched un-flags an in-flight answer after a failed resolve RPC
// so the user can retry (contract D item 5).
func (m *ACPPermissionModal) ResetDispatched(id string) {
	for i := range m.queue {
		if m.queue[i].ID == id {
			m.queue[i].answerDispatched = false
			return
		}
	}
}

func (m *ACPPermissionModal) head() *acpPendingRequest {
	if len(m.queue) == 0 {
		return nil
	}
	return &m.queue[0]
}

// View renders the modal centered on screen (ConfirmModal layout,
// ModalBox/ModalTitle styles). All UI text lowercase per repo convention.
func (m *ACPPermissionModal) View(screenW, screenH int) string {
	head := m.head()
	if head == nil {
		return ""
	}

	var b strings.Builder

	titleStyle := m.styles.ModalTitle.Width(m.width - 4)
	b.WriteString(titleStyle.Render("acp permission request"))
	b.WriteString("\n")
	b.WriteString(m.styles.Muted.Render(strings.Repeat("─", m.width-4)))
	b.WriteString("\n\n")

	lines := []string{
		"agent: " + head.AgentID,
		"session: " + head.SessionID,
		"request: " + head.ID,
		"created: " + formatACPCreated(head.CreatedAt),
	}
	for _, l := range lines {
		b.WriteString(m.styles.Paragraph.Render(truncateForDisplay(l, m.width-4)))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.styles.Paragraph.Render("summary: " + truncateForDisplay(head.Summary, m.width-4)))
	b.WriteString("\n\n")

	if len(m.queue) > 1 {
		b.WriteString(m.styles.Muted.Render(fmt.Sprintf("(%d more in queue)", len(m.queue)-1)))
		b.WriteString("\n")
	}
	b.WriteString(m.styles.Warning.Render(m.PendingLine()))
	b.WriteString("\n\n")

	hintStyle := m.styles.Muted.Align(lipgloss.Center).Width(m.width - 4)
	// Conditional footer (acp-always-allow leaf 02, Contract B): the `a`
	// hint appears ONLY when the head carries an allow_always-kind option
	// — an agent-declared capability. Agents without the option keep the
	// 3-key footer (graceful degradation; `a` still works as allow-once
	// semantics daemon-side, it is simply not advertised).
	footer := "[y] allow · [n] deny · [esc] leave pending"
	if headOffersAlways(head.Options) {
		footer = "[y] allow · [a] always · [n] deny · [esc] leave pending"
	}
	b.WriteString(hintStyle.Render(footer))

	boxStyle := m.styles.ModalBox.Width(m.width)
	content := boxStyle.Render(b.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// formatACPCreated renders the RFC3339 created_at stamp compactly for the
// modal; falls back to the raw string when unparseable (the wire value is
// daemon-authored, so this is display polish only, never a failure).
func formatACPCreated(raw string) string {
	if raw == "" {
		return "unknown"
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return raw
}

// ---------------------------------------------------------------------------
// App-side relay plumbing (Contract D). Kept in this file so app.go carries
// only the pump/key-routing hooks.
// ---------------------------------------------------------------------------

// ACPPermissionResolveResultMsg is the outcome of an
// acp.permissions.resolve RPC dispatched by an answer key press.
type ACPPermissionResolveResultMsg struct {
	ID       string
	Decision string
	Err      error
}

// handleACPPermissionRequested decodes a requested frame's payload map
// (frozen bus.ACPPermissionEvent keys) into the FIFO queue and opens the
// modal on the head. Returns nil (no async work; the modal render is
// synchronous).
func (a *App) handleACPPermissionRequested(payload map[string]any) tea.Cmd {
	id, _ := payload["id"].(string)
	if id == "" {
		return nil
	}
	if a.acpPermissionModal == nil {
		a.acpPermissionModal = NewACPPermissionModal(a.styles)
	}
	agentID, _ := payload["agent_id"].(string)
	sessionID, _ := payload["session_id"].(string)
	summary, _ := payload["summary"].(string)
	createdAt, _ := payload["created_at"].(string)
	a.acpPermissionModal.Enqueue(acpPendingRequest{
		ID:        id,
		AgentID:   agentID,
		SessionID: sessionID,
		Summary:   summary,
		CreatedAt: createdAt,
		Options:   decodeACPPermissionOptions(payload["options"]),
	})
	a.acpPermPending = a.acpPermissionModal.Count()
	// The modal overlay owns the pending line while open; open it on the
	// head unless another modal already has focus (a live modal must not
	// be stomped — the queue + status token still surface the request).
	if a.activeModal == ModalNone {
		a.activeModal = ModalACPPermission
	}
	return nil
}

// handleACPPermissionResolved removes the answered/timed-out request from
// the queue, dismisses the modal when its head was showing that id (or the
// queue drained), and toasts the decision (Contract D item 3). Returns the
// toast-expiry cmd.
func (a *App) handleACPPermissionResolved(payload map[string]any) tea.Cmd {
	id, _ := payload["id"].(string)
	if id == "" || a.acpPermissionModal == nil {
		return nil
	}
	decision, _ := payload["decision"].(string)
	reason, _ := payload["reason"].(string)
	// Approver names WHO answered on another surface (kernel-verified
	// socket peer or HTTP key fingerprint — audit M5 attribution), riding
	// the additive bus key from bus.ACPPermissionEvent.
	approver, _ := payload["approver"].(string)

	showingHead := a.activeModal == ModalACPPermission &&
		a.acpPermissionModal.Count() > 0 &&
		a.acpPermissionModal.queue[0].ID == id
	was := a.acpPermissionModal.Remove(id)
	a.acpPermPending = a.acpPermissionModal.Count()

	var expiryCmd tea.Cmd
	if a.notifications != nil {
		level := components.NotifySuccess
		if decision == "deny" {
			level = components.NotifyError
		}
		title := "acp permission resolved"
		body := id
		if decision != "" {
			body = fmt.Sprintf("%s: %s", decision, id)
		}
		if reason != "" {
			body = fmt.Sprintf("%s (%s)", body, reason)
		}
		if approver != "" {
			body = fmt.Sprintf("%s by %s", body, approver)
		}
		_, expiryCmd = a.notifications.Push(level, title, body)
	}

	if !was {
		return expiryCmd
	}
	if showingHead && a.acpPermPending == 0 {
		// Queue drained: dismiss. With work still queued the FIFO walk
		// keeps the modal open and simply advances to the next head.
		a.activeModal = ModalNone
	}
	return expiryCmd
}

// handleACPPermissionModalKey routes keys while the relay modal is open.
// Answer keys dispatch the resolve RPC asynchronously (never block the
// event loop on socket I/O).
func (a *App) handleACPPermissionModalKey(keyStr string) tea.Cmd {
	action, id := a.acpPermissionModal.HandleKey(keyStr)
	switch action {
	case "dismiss":
		// esc: leave everything pending; the queue survives and the
		// status bar carries "acp perms: N pending" until the next frame.
		a.activeModal = ModalNone
		return nil
	case "allow", "always", "deny":
		return a.rpcResolveACPPermission(id, action)
	}
	return nil
}

// rpcResolveACPPermission performs acp.permissions.resolve via RPCClient
// (the same unix-socket lane every TUI mutation uses, rpc.go Call).
func (a *App) rpcResolveACPPermission(id, decision string) tea.Cmd {
	return func() tea.Msg {
		_, err := a.rpc.Call("acp.permissions.resolve", map[string]any{
			"id":       id,
			"decision": decision,
		})
		return ACPPermissionResolveResultMsg{ID: id, Decision: decision, Err: err}
	}
}

// handleACPPermissionResolveResult processes the resolve RPC outcome.
// Success is quiet — the acp.permission.resolved frame (which also carries
// the decision/reason for the toast and the queue walk) is the terminal
// signal. Failure keeps the modal open, RESETS the answer flag so the key
// press can retry, and toasts the error (Contract D item 5).
func (a *App) handleACPPermissionResolveResult(msg ACPPermissionResolveResultMsg) tea.Cmd {
	if msg.Err == nil {
		return nil
	}
	if a.acpPermissionModal != nil {
		a.acpPermissionModal.ResetDispatched(msg.ID)
	}
	a.statusMessage = fmt.Sprintf("acp resolve failed: %v", msg.Err)
	a.statusMessageTime = time.Now()
	if a.notifications != nil {
		_, expiryCmd := a.notifications.Push(components.NotifyError, "acp resolve failed",
			fmt.Sprintf("%s (%s): %v", msg.ID, msg.Decision, msg.Err))
		a.activeModal = ModalACPPermission
		return expiryCmd
	}
	a.activeModal = ModalACPPermission
	return tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
		return StatusMessageClearMsg{}
	})
}
