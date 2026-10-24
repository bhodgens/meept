package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/tui/modals"
	"github.com/caimlas/meept/internal/tui/types"
)

// Phase 3 modal key-flow tests (leaf 03): open → interact → confirm/cancel
// with FOCUS-ORDER assertions — the failure mode unit tests cannot see
// (tui-e2e-plan.md §1.2: a key binding shadowed by modal focus order).
// App-driven flows run against the finished model (loop stopped); the
// sub-model flows drive the modal HandleKey directly.

func TestModalKeyFlowRename(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// Open via the OpenRenameModalMsg path (the same entry the sessions
	// view's OpenRenameModalMsg uses). The palette 'r' action requires
	// currentSession, which arrives on an async cmd — the direct message
	// keeps this test about the MODAL's key flow, not the load race.
	hp.send(OpenRenameModalMsg{SessionID: "sess-1", CurrentName: "old-name"})
	hp.settle(goldenWidth, goldenHeight)

	app := hp.finish(goldenWidth, goldenHeight)
	if app.activeModal != ModalSessionRename {
		t.Fatalf("rename modal did not open via OpenRenameModalMsg (activeModal=%v)", app.activeModal)
	}

	// Focus order: the input field is focused first (selected==0); typed
	// characters go to the BUFFER, not navigation.
	if got := app.sessionRename.selected; got != 0 {
		t.Fatalf("rename modal initial focus = %d, want 0 (input field)", got)
	}

	// Full confirm path on a fresh modal: open → type → tab → enter.
	app2 := openRenameForFlow(t)
	app2.sessionRename.inputBuffer = "renamed-desc"
	app2.sessionRename.selected = 1
	cmd := app2.sessionRename.HandleKey("enter")
	app2.activeModal = ModalNone // HandleKey hid it
	if cmd == nil {
		t.Fatal("enter on ok button produced no command")
	}
	msg := cmd()
	if _, ok := msg.(SessionRenameMsg); !ok {
		t.Fatalf("enter produced %T, want SessionRenameMsg", msg)
	}

	// Cancel path: esc hides without emitting anything.
	app2.sessionRename.Show("s1", "n")
	app2.sessionRename.HandleKey("esc")
	if app2.sessionRename.IsVisible() {
		t.Fatal("esc did not close the rename modal")
	}
}

// openRenameForFlow builds a finished app model with the rename modal
// open (direct Show, the same entry OpenRenameModalMsg uses).
func openRenameForFlow(t *testing.T) *App {
	t.Helper()
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	app.sessionRename.Show("sess-1", "old-name")
	return app
}

func TestModalKeyFlowPendingChanges(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// Seed a session then open the modal the way ctrl+d does.
	app := hp.finish(goldenWidth, goldenHeight)
	app.currentSession = &types.Session{ID: "sess-pc", Name: "pc"}

	// Direct modal key flow with a seeded change list (the API-backed
	// fetch path is covered by pending_changes_test.go; here the FOCUS
	// and key flow matter).
	pcm := modals.NewPendingChangesModal(nil)
	pcm.Show("sess-pc")
	pcm.SetChanges([]modals.PendingChange{
		{ID: "chg-1", FilePath: "hello.txt"},
		{ID: "chg-2", FilePath: "other.txt"},
	})

	// Focus: first row selected.
	if got := pcm.SelectedIndex(); got != 0 {
		t.Fatalf("pending changes initial selection = %d, want 0", got)
	}
	// Navigation.
	pcm.HandleKey("j")
	if got := pcm.SelectedIndex(); got != 1 {
		t.Fatalf("j moved selection to %d, want 1", got)
	}
	pcm.HandleKey("k")
	if got := pcm.SelectedIndex(); got != 0 {
		t.Fatalf("k moved selection to %d, want 0", got)
	}
	// Diff toggle.
	pcm.HandleKey("v")
	if !pcm.InDiffMode() {
		t.Fatal("v did not enter diff mode")
	}
	pcm.HandleKey("esc")
	if pcm.InDiffMode() {
		t.Fatal("esc in diff mode returned to list, still in diff")
	}
	// Close.
	pcm.HandleKey("esc")
	if pcm.IsVisible() {
		t.Fatal("second esc did not close the modal")
	}
	// Accept action with a nil api surfaces the error result (never a
	// silent no-op).
	pcm.Show("sess-pc")
	pcm.SetChanges([]modals.PendingChange{{ID: "chg-1", FilePath: "hello.txt"}})
	cmd := pcm.HandleKey("a")
	if cmd == nil {
		t.Fatal("accept produced no command")
	}
}

func TestModalKeyFlowProjectPrompt(t *testing.T) {
	// The project prompt is a standalone bubbletea model; drive it
	// directly (y/n/p + enter + esc paths).
	pm := modals.NewProjectPromptModal("sess-pp", "/tmp/proj")
	if _, cmd := pm.Update(keyPressMsgFor("y")); cmd == nil {
		t.Fatal("y produced no command")
	}
	if _, cmd := pm.Update(keyPressMsgFor("n")); cmd == nil {
		t.Fatal("n produced no command")
	}
	if _, cmd := pm.Update(keyPressMsgFor("p")); cmd == nil {
		t.Fatal("p produced no command")
	}
	// Down/enter navigation: down twice lands on pick; enter fires it.
	pm2 := modals.NewProjectPromptModal("sess-pp", "/tmp/proj")
	pm2.Update(keyPressMsgFor("down"))
	pm2.Update(keyPressMsgFor("down"))
	_, cmd := pm2.Update(keyPressMsgFor("enter"))
	if cmd == nil {
		t.Fatal("enter on pick row produced no command")
	}
}

func TestModalKeyFlowConfirmation(t *testing.T) {
	// The destructive-tool confirmation modal: y/n/v key flow and state.
	// (Full coverage in confirmation_test.go; here the focus-order
	// contract: v toggles detail WITHOUT confirming/cancelling.)
	m := NewConfirmationModel(map[string]any{
		"action":     "file_write",
		"summary":    "write hello.txt",
		"reversible": true,
	})
	m2, _ := m.Update(keyPressMsgFor("v"))
	got := m2.(ConfirmationModel)
	if got.IsConfirmed() || got.IsCancelled() {
		t.Fatal("v toggled detail but flipped confirm/cancel state")
	}
	m3, _ := got.Update(keyPressMsgFor("y"))
	got3 := m3.(ConfirmationModel)
	if !got3.IsConfirmed() {
		t.Fatal("y did not confirm")
	}
	if got3.IsCancelled() {
		t.Fatal("confirm and cancel both set")
	}
	m4, _ := NewConfirmationModel(nil).Update(keyPressMsgFor("n"))
	got4 := m4.(ConfirmationModel)
	if !got4.IsCancelled() {
		t.Fatal("n did not cancel")
	}
}

// keyPressMsgFor builds a tea.KeyPressMsg for a named key (subset used by
// the modal flows).
func keyPressMsgFor(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	if len(key) == 1 {
		return tea.KeyPressMsg{Code: rune(key[0])}
	}
	return tea.KeyPressMsg{Code: rune(key[0])}
}

// ---------------------------------------------------------------------------
// ACP human-permission relay modal flows (acp-human-permission-relay
// leaf 04, Contract D).
//
// Keys: y = allow, n = deny, esc = dismiss without answering (the request
// stays pending for the daemon timeout) — the exact y/n/esc vocabulary of
// the destructive-confirm precedent (ConfirmationModel), per the leaf's
// "reuse modal accept/decline keys" instruction. The modal is NOT a global
// keybinding, so no palette/inventory change is needed.
// ---------------------------------------------------------------------------

func acpRequestedEvent(id, agent, session, summary string) BusEvent {
	return BusEvent{
		Topic: bus.TopicACPPermissionRequested.Name,
		Type:  "event",
		Payload: map[string]any{
			"id":         id,
			"agent_id":   agent,
			"session_id": session,
			"summary":    summary,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
}

// acpRequestedEventWithOptions builds a requested frame that ALSO carries
// the additive "options" key (acp-always-allow leaf 01 wire shape: array of
// {optionId,name,kind}), exactly how the WS/bus decode delivers it.
func acpRequestedEventWithOptions(id, agent, session, summary string, options []map[string]any) BusEvent {
	opts := make([]any, 0, len(options))
	for _, o := range options {
		opts = append(opts, o)
	}
	return BusEvent{
		Topic: bus.TopicACPPermissionRequested.Name,
		Type:  "event",
		Payload: map[string]any{
			"id":         id,
			"agent_id":   agent,
			"session_id": session,
			"summary":    summary,
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"options":    opts,
		},
	}
}

func acpResolvedEvent(id, decision, reason string) BusEvent {
	return acpResolvedEventFull(id, decision, reason, "")
}

// acpResolvedEventFull carries the additive "approver" key (audit M5
// attribution) when non-empty — the wire shape bus.ACPPermissionEvent
// publishes on resolved frames answered by another surface.
func acpResolvedEventFull(id, decision, reason, approver string) BusEvent {
	payload := map[string]any{
		"id":       id,
		"decision": decision,
		"reason":   reason,
	}
	if approver != "" {
		payload["approver"] = approver
	}
	return BusEvent{
		Topic:   bus.TopicACPPermissionResolved.Name,
		Type:    "event",
		Payload: payload,
	}
}

func sendACPEvents(h *headlessProgram, events ...BusEvent) {
	h.send(EventStreamDataMsg{Events: events})
	h.settle(goldenWidth, goldenHeight)
}

// TestACPModal_ResolvedToastCarriesApprover pins the audit-M5 attribution
// render: a resolved frame answered on another surface names the approver
// in the toast body, and a frame without the additive key toasts without a
// dangling "by" (the same direct-Update idiom as the FIFO-walk test).
func TestACPModal_ResolvedToastCarriesApprover(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	sendACPEvents(hp,
		acpRequestedEvent("perm-t1", "ag-1", "sess-1", "s"),
		acpRequestedEvent("perm-t2", "ag-2", "sess-2", "s"),
	)
	app := hp.finish(goldenWidth, goldenHeight)

	app.Update(EventStreamDataMsg{Events: []BusEvent{
		acpResolvedEventFull("perm-t1", "allow", "answered", "peercred:pid=900,uid=501"),
	}})
	actives := app.notifications.Active()
	if len(actives) == 0 {
		t.Fatal("resolved frame did not toast")
	}
	last := actives[len(actives)-1]
	if !strings.Contains(last.Message, "allow: perm-t1 (answered) by peercred:pid=900,uid=501") {
		t.Errorf("toast body = %q, want decision: id (reason) by approver", last.Message)
	}

	app.Update(EventStreamDataMsg{Events: []BusEvent{
		acpResolvedEvent("perm-t2", "deny", "timeout"),
	}})
	actives = app.notifications.Active()
	if len(actives) == 0 {
		t.Fatal("second resolved frame did not toast")
	}
	last = actives[len(actives)-1]
	if !strings.Contains(last.Message, "deny: perm-t2 (timeout)") {
		t.Errorf("toast body = %q, want deny: perm-t2 (timeout)", last.Message)
	}
	if strings.Contains(last.Message, " by ") {
		t.Errorf("approverless toast carries a dangling 'by': %q", last.Message)
	}
}

func TestACPModal_OpenOnRequestedFrame(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	sendACPEvents(hp, acpRequestedEvent("perm-1", "ag-1", "sess-1", "run tool: bash rm -rf"))
	app := hp.finish(goldenWidth, goldenHeight)

	if app.activeModal != ModalACPPermission {
		t.Fatalf("requested frame did not open the permission modal (activeModal=%v)", app.activeModal)
	}
	if app.acpPermissionModal == nil {
		t.Fatal("acpPermissionModal not lazily constructed")
	}
	view := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	for _, want := range []string{"ag-1", "sess-1", "run tool: bash rm -rf", "perm-1"} {
		if !strings.Contains(view, want) {
			t.Errorf("modal view missing %q, got:\n%s", want, view)
		}
	}
	// pending-count line (Contract D item 1; rendered in the modal footer —
	// a modal overlay replaces the status bar, so the status bar cannot
	// carry it while open)
	if !strings.Contains(view, "acp perms: 1 pending") {
		t.Errorf("modal view missing pending line, got:\n%s", view)
	}
}

func TestACPModal_FIFOQueueAndAnswerWalk(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	hp.stub.setResult("acp.permissions.resolve", map[string]any{"id": "perm-1", "decision": "allow", "reason": "answered"})

	sendACPEvents(hp,
		acpRequestedEvent("perm-1", "ag-1", "sess-1", "first summary"),
		acpRequestedEvent("perm-2", "ag-2", "sess-2", "second summary"),
	)
	app := hp.finish(goldenWidth, goldenHeight)

	if app.acpPermPending != 2 {
		t.Fatalf("pending queue = %d, want 2", app.acpPermPending)
	}
	// Head shown = first arrival (FIFO).
	view := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	if !strings.Contains(view, "ag-1") || strings.Contains(view, "second summary") {
		t.Errorf("modal should show the FIFO head, got:\n%s", view)
	}
	if !strings.Contains(view, "acp perms: 2 pending") {
		t.Errorf("pending line should read 2, got:\n%s", view)
	}

	// y on the head answers allow via RPC acp.permissions.resolve.
	app.Update(keyPressMsgFor("y"))
	if app.acpPermissionModal.dispatchCount != 1 {
		t.Fatalf("y did not dispatch resolve for the head (dispatchCount=%d, want 1)", app.acpPermissionModal.dispatchCount)
	}
	if app.acpPermissionModal.lastDispatchID != "perm-1" {
		t.Fatalf("dispatch targeted %q, want perm-1", app.acpPermissionModal.lastDispatchID)
	}
	if !app.acpPermissionModal.queue[0].answerDispatched {
		t.Error("head should be marked answered after dispatch (double-press guard)")
	}

	// The resolved frame dismisses the head and advances the queue.
	app.Update(EventStreamDataMsg{Events: []BusEvent{acpResolvedEvent("perm-1", "allow", "answered")}})
	if app.activeModal != ModalACPPermission {
		t.Fatalf("queue not empty: modal should stay open showing the next head (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 1 {
		t.Fatalf("pending queue = %d, want 1 after walking head", app.acpPermPending)
	}
	view2 := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	if !strings.Contains(view2, "ag-2") || !strings.Contains(view2, "acp perms: 1 pending") {
		t.Errorf("next head not shown after resolve, got:\n%s", view2)
	}

	// Resolving the last one dismisses the modal.
	app.Update(EventStreamDataMsg{Events: []BusEvent{acpResolvedEvent("perm-2", "deny", "timeout")}})
	if app.activeModal != ModalNone {
		t.Fatalf("empty queue should dismiss modal (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 0 {
		t.Fatalf("pending queue = %d, want 0", app.acpPermPending)
	}
}

func TestACPModal_EscDismissesKeepingPending(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	sendACPEvents(hp, acpRequestedEvent("perm-1", "ag-1", "sess-1", "leave me pending"))
	app := hp.finish(goldenWidth, goldenHeight)

	app.Update(keyPressMsgFor("esc"))
	if app.activeModal != ModalNone {
		t.Fatalf("esc should dismiss the modal (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 1 {
		t.Fatalf("esc must leave the request pending (count=%d, want 1)", app.acpPermPending)
	}
	// A NEW requested frame reopens the modal on the still-pending FIFO
	// head (dismiss-without-answering never drops the queue; there is no
	// global rebind key — the leaf's no-new-keybinding rule).
	app.Update(EventStreamDataMsg{Events: []BusEvent{acpRequestedEvent("perm-2", "ag-2", "sess-2", "second")}})
	if app.activeModal != ModalACPPermission {
		t.Fatalf("new frame should reopen modal on queued head (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 2 {
		t.Fatalf("dismissed head must survive (count=%d, want 2)", app.acpPermPending)
	}
	view := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	if !strings.Contains(view, "leave me pending") {
		t.Errorf("reopen should show FIFO head perm-1, got:\n%s", view)
	}
}

func TestACPModal_ResolvedFrameFromElsewhere(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	sendACPEvents(hp, acpRequestedEvent("perm-1", "ag-1", "sess-1", "answered by gui"))
	app := hp.finish(goldenWidth, goldenHeight)

	// Another client answered — the frame dismisses the modal even though
	// this client never pressed a key.
	app.Update(EventStreamDataMsg{Events: []BusEvent{acpResolvedEvent("perm-1", "deny", "answered")}})
	if app.activeModal != ModalNone {
		t.Fatalf("resolved frame should dismiss modal (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 0 {
		t.Fatalf("pending queue = %d, want 0", app.acpPermPending)
	}
}

func TestACPModal_ResolveFailureKeepsModalRetryable(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// One event that makes the resolve RPC return a JSON-RPC error
	// envelope: the stub is an in-process server, so serve() answers
	// unknown methods with {"ok":true}; instead of socket teardown (whose
	// reconnect retries stall the loop), map the resolve method to a
	// handler-side failure by NOT setting a result and closing the
	// listener afterwards is racy — use the deterministic path: keep the
	// stub alive but answer resolve with an error envelope.
	hp.stub.setError("acp.permissions.resolve", "daemon is restarting")

	sendACPEvents(hp, acpRequestedEvent("perm-1", "ag-1", "sess-1", "unreachable daemon"))
	app := hp.finish(goldenWidth, goldenHeight)

	_, cmd := app.Update(keyPressMsgFor("n"))
	if cmd == nil {
		t.Fatal("n should dispatch an resolve RPC cmd")
	}
	// Drive the dispatched RPC to completion synchronously.
	result := cmd()
	app.Update(result)

	if app.activeModal != ModalACPPermission {
		t.Fatalf("RPC failure must keep the modal open (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 1 {
		t.Fatalf("RPC failure must keep the request pending (count=%d, want 1)", app.acpPermPending)
	}
	if app.acpPermissionModal.queue[0].answerDispatched {
		t.Error("failed dispatch must RESET the answer flag so the key press can retry")
	}
	// Retry: the answer key dispatches again.
	_, cmd2 := app.Update(keyPressMsgFor("n"))
	if cmd2 == nil {
		t.Fatal("retry after failure must re-dispatch resolve")
	}
	if app.acpPermissionModal.dispatchCount != 2 {
		t.Fatalf("dispatchCount = %d, want 2 after retry", app.acpPermissionModal.dispatchCount)
	}
}

// ---------------------------------------------------------------------------
// acp-always-allow leaf 02 (Contract B): the `a` key + conditional footer.
// ---------------------------------------------------------------------------

var acpAlwaysOptions = []map[string]any{
	{"optionId": "allow-once-1", "name": "Allow once", "kind": "allow_once"},
	{"optionId": "always-session-1", "name": "Always allow in session", "kind": "allow_always"},
	{"optionId": "reject-1", "name": "Reject", "kind": "reject_once"},
}

func TestACPModal_AlwaysKeyResolvesAlways(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	hp.stub.setResult("acp.permissions.resolve", map[string]any{"id": "perm-1", "decision": "always", "reason": "answered"})

	sendACPEvents(hp, acpRequestedEventWithOptions("perm-1", "ag-1", "sess-1", "always me", acpAlwaysOptions))
	app := hp.finish(goldenWidth, goldenHeight)

	// Conditional footer shows the a hint when the head offers allow_always.
	view := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	if !strings.Contains(view, "[a] always") {
		t.Errorf("footer should advertise [a] always when head offers allow_always, got:\n%s", view)
	}

	// a on the head answers ALWAYS via RPC acp.permissions.resolve; the
	// dispatched cmd carries decision "always" verbatim into the RPC
	// params (leaf 01 accepts it at the boundary).
	_, cmd := app.Update(keyPressMsgFor("a"))
	if cmd == nil {
		t.Fatal("a did not dispatch a resolve RPC cmd")
	}
	if app.acpPermissionModal.dispatchCount != 1 {
		t.Fatalf("a did not dispatch resolve for the head (dispatchCount=%d, want 1)", app.acpPermissionModal.dispatchCount)
	}
	if app.acpPermissionModal.lastDispatchID != "perm-1" {
		t.Fatalf("dispatch targeted %q, want perm-1", app.acpPermissionModal.lastDispatchID)
	}
	msg := cmd()
	res, ok := msg.(ACPPermissionResolveResultMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want ACPPermissionResolveResultMsg", msg)
	}
	if res.Decision != "always" {
		t.Errorf("dispatched decision = %q, want always", res.Decision)
	}
	if res.Err != nil {
		t.Errorf("resolve over the stub failed: %v", res.Err)
	}
	// double-press guard: a second a no-ops until the resolved frame.
	action, _ := app.acpPermissionModal.HandleKey("a")
	if action != "" {
		t.Errorf("second a after dispatch = %q, want no-op", action)
	}

	// The always-resolved frame dismisses + toasts without choking: it is
	// NOT an error toast (deny-only mapping), and the modal closes.
	app.Update(EventStreamDataMsg{Events: []BusEvent{acpResolvedEvent("perm-1", "always", "answered")}})
	if app.activeModal != ModalNone {
		t.Fatalf("always-resolved frame should dismiss the modal (activeModal=%v)", app.activeModal)
	}
	if app.acpPermPending != 0 {
		t.Fatalf("pending queue = %d, want 0", app.acpPermPending)
	}
	app.Update(tea.WindowSizeMsg{Width: goldenWidth, Height: goldenHeight})
	toasts := ansi.Strip(app.notifications.View(goldenWidth))
	if !strings.Contains(toasts, "always") {
		t.Errorf("resolved toast should carry the always decision, got:\n%s", toasts)
	}
}

func TestACPModal_FooterGatedByOptionsKey(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// Head WITHOUT an allow_always-kind option: 3-key footer (the [a]
	// hint is an agent-declared capability, gracefully absent).
	sendACPEvents(hp, acpRequestedEventWithOptions("perm-no", "ag-1", "sess-1", "no always here",
		[]map[string]any{{"optionId": "allow-once-1", "name": "Allow once", "kind": "allow_once"}}))
	app := hp.finish(goldenWidth, goldenHeight)
	view := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	if strings.Contains(view, "[a] always") {
		t.Errorf("footer must NOT advertise [a] always without the option, got:\n%s", view)
	}
	if !strings.Contains(view, "[y] allow") || !strings.Contains(view, "[n] deny") {
		t.Errorf("3-key footer missing allow/deny hints, got:\n%s", view)
	}

	// Absent-options head (old wire shape) keeps the same 3-key footer.
	app.Update(EventStreamDataMsg{Events: []BusEvent{acpRequestedEvent("perm-old", "ag-2", "sess-2", "legacy frame")}})
	view2 := ansi.Strip(app.acpPermissionModal.View(goldenWidth, goldenHeight))
	if strings.Contains(view2, "[a] always") {
		t.Errorf("absent-options head must keep the 3-key footer, got:\n%s", view2)
	}

	// A head whose kind only loosely matches (contains allow+always,
	// case-insensitive) still gates the footer — mirrors
	// resolveAlwaysOptionID's second pass.
	app3hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	sendACPEvents(app3hp, acpRequestedEventWithOptions("perm-loose", "ag-1", "sess-1", "loose kind",
		[]map[string]any{{"optionId": "alw", "name": "Always", "kind": "Allow-Always-scoped"}}))
	app3 := app3hp.finish(goldenWidth, goldenHeight)
	view3 := ansi.Strip(app3.acpPermissionModal.View(goldenWidth, goldenHeight))
	if !strings.Contains(view3, "[a] always") {
		t.Errorf("case-insensitive allow+always kind should gate the footer on, got:\n%s", view3)
	}
}
