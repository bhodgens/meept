package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

// cloneTestDrainer is the daemon-package mirror of agent's mockMessageDrainer:
// it hands out canned turn-start messages once, then drains empty. It
// satisfies agent.MessageDrainer without importing agent's unexported test
// helpers.
type cloneTestDrainer struct {
	msgs  []agent.IncomingAgentMessage
	calls int
}

func (d *cloneTestDrainer) DrainInbox(to string, limit int) ([]agent.IncomingAgentMessage, error) {
	d.calls++
	out := d.msgs
	d.msgs = nil
	return out, nil
}

// TestClone_InheritsDrainer pins C4's drainer-propagation half: a per-turn
// employee clone must carry the shared template's MessageDrainer so the
// clone's turn-start prompt build drains the queued message (not a tool
// call). RED until MessageDrainer() exists and employeeTurnClone forwards it.
func TestClone_InheritsDrainer(t *testing.T) {
	template := agent.NewAgentLoop("template-sess", t.TempDir(), agent.WithAgentID("worker"))
	if template == nil {
		t.Fatal("NewAgentLoop(template) returned nil")
	}

	mock := &cloneTestDrainer{msgs: []agent.IncomingAgentMessage{
		{ID: "msg-clone-1", From: "orchestrator", To: "worker", Body: "delivered at turn start"},
	}}
	template.SetMessageDrainer(mock)

	clone := employeeTurnClone(template, "conv-clone", "worker", nil)
	if clone == nil {
		t.Fatal("employeeTurnClone returned nil for a non-nil template")
	}

	// The clone's drainer is the same source the turn-start prompt build
	// consumes (buildAgentMessagesSection reads l.messageDrainer). Reading
	// it through the getter asserts propagation without touching the inbox
	// tool. RED: MessageDrainer getter is undefined until the impl leaf lands.
	got := clone.MessageDrainer()
	if got == nil {
		t.Fatal("clone did not inherit the template's message drainer")
	}

	msgs, err := got.DrainInbox("worker", 20)
	if err != nil {
		t.Fatalf("DrainInbox: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("drained %d messages; want 1 (turn-start delivery)", len(msgs))
	}
	if msgs[0].ID != "msg-clone-1" || msgs[0].From != "orchestrator" || msgs[0].Body != "delivered at turn start" {
		t.Fatalf("inherited drainer returned wrong message: %+v", msgs[0])
	}
	if mock.calls != 1 {
		t.Fatalf("template drainer drain calls = %d; want 1", mock.calls)
	}
}
