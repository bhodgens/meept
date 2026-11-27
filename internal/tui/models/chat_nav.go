package models

// 05-navigation leaf T2/T3: task chip navigation + reply read surfaces.
//
// A completed task reply carries its C1 correlation fields (T1); this
// file renders the tappable task chip, emits the navigation signal on
// Enter, and wires the two lazy read surfaces behind the existing
// expand ('e') and a new route key ('w'):
//   - tool-call trace (C4): session.messages.get with
//     include_tool_calls=true, rows rendered under the expanded reply;
//   - why-this-route popover (C5): session.dispatch_trace for the
//     reply's session, intent/method/confidence rows.
// Fetches are once-per-message (bounded: no per-render RPC storm).

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
)

// TaskChipNavMsg is emitted when the user activates the task chip on a
// reply bubble (Enter on the selected message). The App routes it to
// the tasks view and opens the detail modal for TaskID (the same
// in-model seam Enter uses inside the tasks view).
type TaskChipNavMsg struct {
	TaskID string
}

// toolCallTrace is one persisted tool call rendered under an expanded
// reply (C4: messages.get include_tool_calls joins these server-side).
type toolCallTrace struct {
	Name          string `json:"name"`
	ArgsSummary   string `json:"args_summary"`
	ResultSummary string `json:"result_summary"`
}

// dispatchTraceRow is one route-evidence entry (C5:
// session.dispatch_trace rows).
type dispatchTraceRow struct {
	TurnID           string  `json:"turn_id"`
	InputSummary     string  `json:"input_summary"`
	IntentType       string  `json:"intent_type"`
	AgentID          string  `json:"agent_id"`
	Confidence       float64 `json:"confidence"`
	ClassifierMethod string  `json:"classifier_method"`
	HandlerCase      string  `json:"handler_case"`
	TaskID           string  `json:"task_id"`
}

// selectedAssistantMessage returns the selected message when it is an
// assistant bubble, else false.
func (m *ChatModel) selectedAssistantMessage() (ChatMessage, int, bool) {
	if m.focused != FocusViewport || m.selectedMsgIdx < 0 || m.selectedMsgIdx >= len(m.messages) {
		return ChatMessage{}, -1, false
	}
	msg := m.messages[m.selectedMsgIdx]
	if msg.Role != RoleAssistant {
		return ChatMessage{}, -1, false
	}
	return msg, m.selectedMsgIdx, true
}

// handleChipEnter emits the task-detail navigation signal when Enter is
// pressed with a task-bearing reply selected (T2). Returns nil cmd when
// the selection carries no task id.
func (m *ChatModel) handleChipEnter() tea.Cmd {
	msg, _, ok := m.selectedAssistantMessage()
	if !ok || msg.TaskID == "" {
		return nil
	}
	return func() tea.Msg { return TaskChipNavMsg{TaskID: msg.TaskID} }
}

// fetchTrace loads the persisted tool-call trace for the selected reply
// through session.messages.get with include_tool_calls=true (C4),
// storing the rows keyed by message id. Runs inside Update on the
// event-loop goroutine (the loopback-daemon round trip is local IPC,
// sub-millisecond; doing it here keeps the model single-goroutine and
// race-free -- no cmd-goroutine mutation). Once per message: the
// fetched ledger bounds the RPC count (no per-render storm).
func (m *ChatModel) fetchTrace(msg ChatMessage) {
	if msg.ID == "" || m.rpc == nil || !m.rpc.IsConnected() {
		return
	}
	if m.traceFetched == nil {
		m.traceFetched = map[string]bool{}
	}
	if m.traceFetched[msg.ID] {
		return
	}
	m.traceFetched[msg.ID] = true
	result, err := m.rpc.Call("session.messages.get", map[string]any{
		"session_id":         m.sessionID,
		"include_tool_calls": true,
	})
	if err != nil {
		return
	}
	var resp struct {
		Messages []struct {
			ToolCalls []struct {
				Name          string `json:"name"`
				ArgsSummary   string `json:"args_summary"`
				ResultSummary string `json:"result_summary"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if json.Unmarshal(result, &resp) != nil {
		return
	}
	var rows []toolCallTrace
	for _, sm := range resp.Messages {
		for _, tc := range sm.ToolCalls {
			rows = append(rows, toolCallTrace{
				Name:          tc.Name,
				ArgsSummary:   tc.ArgsSummary,
				ResultSummary: tc.ResultSummary,
			})
		}
	}
	if m.messageTraces == nil {
		m.messageTraces = map[string][]toolCallTrace{}
	}
	m.messageTraces[msg.ID] = rows
}

// fetchRoute loads why-this-route evidence for the reply's session
// through session.dispatch_trace (C5), storing rows keyed by message
// id. Same Update-goroutine + once-per-message discipline as
// fetchTrace.
func (m *ChatModel) fetchRoute(msg ChatMessage) {
	if m.rpc == nil || !m.rpc.IsConnected() {
		return
	}
	if m.routeFetched == nil {
		m.routeFetched = map[string]bool{}
	}
	key := msg.ID
	if key == "" {
		key = msg.Content
	}
	if m.routeFetched[key] {
		return
	}
	m.routeFetched[key] = true
	result, err := m.rpc.Call("session.dispatch_trace", map[string]any{
		"session_id": m.sessionID,
	})
	if err != nil {
		return
	}
	var resp struct {
		Entries []dispatchTraceRow `json:"entries"`
	}
	if json.Unmarshal(result, &resp) != nil {
		return
	}
	if m.routeEvidence == nil {
		m.routeEvidence = map[string][]dispatchTraceRow{}
	}
	m.routeEvidence[key] = resp.Entries
}

// renderTraceRowsFor returns the stored trace rows for a message.
func (m *ChatModel) renderTraceRowsFor(msg ChatMessage) string {
	if msg.ID == "" || len(m.messageTraces) == 0 {
		return ""
	}
	return renderTraceRows(m.messageTraces[msg.ID])
}

// renderRouteRowsFor returns the stored route-evidence panel for a
// message.
func (m *ChatModel) renderRouteRowsFor(msg ChatMessage) string {
	key := msg.ID
	if key == "" {
		key = msg.Content
	}
	if len(m.routeEvidence) == 0 {
		return ""
	}
	return renderRouteRows(m.routeEvidence[key])
}

// applyRestoredClassifications stamps the C1 mode-badge fields
// (intent_type / handler_case) onto restored assistant bubbles from the
// session's persisted dispatch rows (session.dispatch_trace, the same
// RPC the 'w' route popover reads). A live turn badges from the
// turn.terminal event (attachTurnCorrelation); a transcript restored
// from the server has no terminal events, so without this pass a
// reopened session renders no mode badges (F13c, 06-planning/03).
//
// JOIN (bughunt 2026-11-24 M12): the row <-> bubble join is
// DETERMINISTIC BY ARRIVAL ORDER, not input text. rows arrive oldest-
// first (the RPC answers newest-first; dispatchTraceHydrateCmd
// reverses). nth stampable assistant bubble <-> nth stampable dispatch
// row, scanned strictly forward. The old prefix-text join misjoined
// identical inputs ("yes" twice) and could badge one bubble from a row
// whose summary merely prefixed another turn's text; text is now ONLY
// a disqualifier — when both sides carry text and it disagrees, the
// join refuses. A legacy S4-privacy row (empty summary) badges BY
// POSITION: at HEAD it matched nothing, so privacy sessions rendered
// zero badges at all. Residual risk (documented deviation): bubbles
// the daemon never persisted (client-local notices) shift the
// positional mapping; they consume no row, so the worst case is an
// unbadged tail, never a wrong badge on an early turn.
//
// Fail-soft by design: a refused join leaves the bubble unclassified —
// an unclassified bubble renders NO badge, never a wrong one.
func (m *ChatModel) applyRestoredClassifications(msgs []ChatMessage, rows []dispatchTraceRow) {
	if len(rows) == 0 {
		return
	}
	consumed := make([]bool, len(rows))
	lastUser := ""
	turnIdx := 0
	for i := range msgs {
		msg := &msgs[i]
		if msg.Role == RoleUser {
			lastUser = msg.Content
			continue
		}
		if msg.Role != RoleAssistant {
			continue
		}
		if msg.IntentType != "" || msg.HandlerCase != "" {
			continue
		}
		if lastUser == "" {
			continue
		}
		for turnIdx < len(rows) &&
			(consumed[turnIdx] || (rows[turnIdx].IntentType == "" && rows[turnIdx].HandlerCase == "")) {
			turnIdx++
		}
		if turnIdx >= len(rows) {
			continue
		}
		row := &rows[turnIdx]
		if row.InputSummary != "" && !dispatchInputMatches(row.InputSummary, lastUser) {
			continue
		}
		msg.IntentType = row.IntentType
		msg.HandlerCase = row.HandlerCase
		msg.rendered = ""
		consumed[turnIdx] = true
		turnIdx++
		lastUser = ""
	}
}

// dispatchTraceHydrateCmd arms the session.dispatch_trace fetch OFF the
// Update event loop (bughunt 2026-11-24 M12 half B: hydration used to
// run this RPC synchronously inside Update on the SessionMessagesLoadedMsg
// path -- a stalled daemon froze every keypress; the transcript itself
// already loads via a tea.Cmd for the same reason, chat.go SetSession).
// The cmd delivers a DispatchTraceLoadedMsg carrying rows in arrival
// order; Update then applies them with applyRestoredClassifications.
// Fail-soft: an RPC error or a bad payload lands with zero rows (no
// stamps, no panic). Returns nil when nothing needs stamping (no RPC).
func (m *ChatModel) dispatchTraceHydrateCmd(sessionID string, msgs []ChatMessage) tea.Cmd {
	if m.rpc == nil || !m.rpc.IsConnected() {
		return nil
	}
	need := false
	for _, msg := range msgs {
		if msg.Role == RoleAssistant && msg.IntentType == "" && msg.HandlerCase == "" {
			need = true
			break
		}
	}
	if !need {
		return nil
	}
	rpc := m.rpc
	return func() tea.Msg {
		result, err := rpc.Call("session.dispatch_trace", map[string]any{
			"session_id": sessionID,
		})
		if err != nil {
			return DispatchTraceLoadedMsg{SessionID: sessionID}
		}
		var resp struct {
			Entries []dispatchTraceRow `json:"entries"`
		}
		if json.Unmarshal(result, &resp) != nil {
			return DispatchTraceLoadedMsg{SessionID: sessionID}
		}
		// The RPC answers newest-first (ORDER BY id DESC); turns join in
		// arrival order.
		rows := make([]dispatchTraceRow, len(resp.Entries))
		for i, e := range resp.Entries {
			rows[len(resp.Entries)-1-i] = e
		}
		return DispatchTraceLoadedMsg{SessionID: sessionID, Rows: rows}
	}
}

// dispatchInputMatches reports whether a dispatch row's bounded input
// summary identifies the same turn as a restored user message. The
// daemon persists either the first sentence (<= 100 chars) or the
// bounded full input (the evidence path, capped at 200 runes), so one
// side is a prefix of the other whenever they name the same turn. Since
// M12 the positional join calls this only as a DISQUALIFIER when the
// row carries text: a disagreement blocks the stamp (no badge rather
// than a wrong one). An empty summary is not consulted — a legacy
// S4-privacy row badges by position.
func dispatchInputMatches(summary, userText string) bool {
	if summary == "" || userText == "" {
		return false
	}
	if len(userText) >= len(summary) {
		return strings.HasPrefix(userText, summary)
	}
	return strings.HasPrefix(summary, userText)
}

// renderTaskChip renders the tappable chip line for a task-bearing
// assistant reply (W14: the reply must surface which task ran).
func renderTaskChip(taskID string) string {
	chip := lipgloss.NewStyle().
		Foreground(paletteColor("info")).
		Bold(true).
		Render(fmt.Sprintf("[task %s]", taskID))
	hint := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Italic(true).
		Render(" (enter) open")
	return "  " + chip + hint
}

// modeBadgeFor maps a C1 terminal classification (intent_type /
// handler_case) to a user-facing mode badge label (06-planning/03 T2,
// F13c/W17): the transcript must delineate planning from direct
// execution. The RAW handler_case enum never reaches the user -- only
// the mapped labels below. Empty classification renders no badge
// (unclassified/legacy terminals stay clean).
func modeBadgeLabel(intentType, handlerCase string) string {
	switch intentType {
	case "plan":
		return "plan"
	case "quickplan", "quick_plan":
		return "quickplan" // gate asserts the "quick" substring; full label satisfies it
	case "recall":
		return "recall"
	case "code", "debug", "git", "fix", "refactor", "analyze", "report", "search", "architect", "compound":
		// Executed turns: distinguish async dispatch from a direct
		// synchronous execution by the handler case, never by leaking it.
		switch handlerCase {
		case "direct_mode", "direct_response":
			return "direct"
		default:
			return "direct"
		}
	case "chat":
		return "direct"
	default:
		// Unknown intent: fall back to the handler case shape only.
		switch handlerCase {
		case "async_dispatch":
			return "plan"
		case "direct_mode", "direct_response":
			return "direct"
		default:
			return ""
		}
	}
}

// renderModeBadge renders the mode badge line under an assistant bubble
// (06-planning/03 T2). No badge for empty classifications.
func renderModeBadge(msg ChatMessage) string {
	label := modeBadgeLabel(msg.IntentType, msg.HandlerCase)
	if label == "" {
		return ""
	}
	badge := lipgloss.NewStyle().
		Foreground(paletteColor("info")).
		Bold(true).
		Render("[" + label + "]")
	return "  " + badge
}

// renderTraceRows renders the persisted tool-call trace under an
// expanded reply (C4).
func renderTraceRows(rows []toolCallTrace) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	label := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Italic(true)
	for _, tc := range rows {
		b.WriteString("  " + label.Render("tool: "+tc.Name) + "\n")
		if tc.ArgsSummary != "" {
			b.WriteString("    " + label.Render("args: "+tc.ArgsSummary) + "\n")
		}
		if tc.ResultSummary != "" {
			b.WriteString("    " + label.Render("result: "+tc.ResultSummary) + "\n")
		}
	}
	return b.String()
}

// renderRouteRows renders the why-this-route evidence panel (C5):
// intent / method / confidence rows for each dispatch-trace entry.
func renderRouteRows(rows []dispatchTraceRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	label := lipgloss.NewStyle().
		Foreground(paletteColor("textMuted")).
		Italic(true)
	b.WriteString(label.Render("--- why this route ---") + "\n")
	for _, e := range rows {
		b.WriteString(fmt.Sprintf("  intent: %s  method: %s  confidence: %.2f  case: %s\n",
			e.IntentType, e.ClassifierMethod, e.Confidence, e.HandlerCase))
	}
	return b.String()
}
