package bus

import (
	"github.com/caimlas/meept/internal/comm/wsclass"
)

// ACPPermissionEvent is the frozen bus/WS payload for the human permission
// relay (acp-human-permission-relay leaf 03, Contract C). The daemon-side
// notifier (internal/daemon/acp_notifier.go) adapts the internal/acp
// PendingPermission value into this shape and publishes it on
// TopicACPPermissionRequested / TopicACPPermissionResolved.
//
// The payload lives in internal/bus — NOT internal/acp — so HTTP/WS/TUI/GUI
// consumers can decode it without pulling the ACP client into their
// dependency closure (internal/acp must not gain a bus dep; the daemon does
// the adaptation, per the leaf's import-cycle note). wsclass imports
// nothing from internal/, so bus -> wsclass is cycle-free (same direction
// as agent -> wsclass).
//
// Wire keys are frozen (leaf 03 task 1): id, agent_id, session_id, summary,
// created_at (+ decision/reason on the resolved frame).
type ACPPermissionEvent struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"` // RFC3339 stamp of registration
	// Decision + Reason are empty on the requested frame and carry the
	// terminal answer ("allow"/"deny", "answered"/"timeout"/"no_relay"/
	// "full") on the resolved frame.
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// WSClass implements wsclass.WSClassified: ACP permission frames are
// lifecycle signals and MUST render as progress — never chat_message
// (AGENTS.md WS classification invariant: a chat_message frame opens a
// blank bubble in the Flutter GUI).
func (ACPPermissionEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }

// TopicACPPermissionRequested fires when a permission_mode "human" (or
// decision-lane escalated) session/requestPermission enters the pending
// registry. TopicACPPermissionResolved fires when the wait terminates
// (answered, timeout, or fail-closed rejections). Typed Topic[T] per
// internal/comm/AGENTS.md: the payload is stable and single-shape.
var (
	TopicACPPermissionRequested = NewTopic[ACPPermissionEvent]("acp.permission.requested")
	TopicACPPermissionResolved  = NewTopic[ACPPermissionEvent]("acp.permission.resolved")
)
