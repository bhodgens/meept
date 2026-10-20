// acp_notifier.go adapts the internal/acp human permission relay seam
// (PermissionNotifier, leaf 02 Contract B) onto the daemon message bus
// (leaf 03, Contract C): every requested/resolved transition becomes a
// typed bus publish that the WS relay forwards to TUI/GUI surfaces
// (leaves 04/05). internal/acp must NOT import internal/bus — this file is
// the adaptation point, and internal/bus owns the payload shape.
package daemon

import (
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/bus"
)

// acpBusNotifier publishes PendingPermission snapshots on the two typed
// acp.permission.* topics. Calls run on the Transport Serve goroutine with
// no session lock held, so they must never block: bus.PublishT is
// non-blocking (drop-on-full), which matches the relay's reconnect
// contract — a missed frame does not cancel the wait, the pending record
// stays answerable via RPC until the timeout.
type acpBusNotifier struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

// compile-time proof the seam matches leaf 02's interface.
var _ acp.PermissionNotifier = (*acpBusNotifier)(nil)

// NotifyPermissionRequested publishes the pending record and reports
// whether ANY consumer received it (PublishT's delivery count). false is
// the "no listener right now" signal — the session keeps waiting so a
// reconnecting GUI can still answer.
func (n *acpBusNotifier) NotifyPermissionRequested(p acp.PendingPermission) bool {
	if n == nil || n.bus == nil {
		return false
	}
	delivered := bus.PublishT(n.bus, bus.TopicACPPermissionRequested, "acp-relay", toACPEvent(p, "", ""))
	return delivered > 0
}

// NotifyPermissionResolved publishes the terminal shape (decision
// "allow"/"deny", reason "answered"/"timeout"/"no_relay"/"full").
func (n *acpBusNotifier) NotifyPermissionResolved(p acp.PendingPermission, decision, reason string) {
	if n == nil || n.bus == nil {
		return
	}
	logger := n.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Debug("acp permission relay resolved",
		"component", "acp",
		"pending", p.ID,
		"agent", p.AgentID,
		"decision", decision,
		"reason", reason,
	)
	bus.PublishT(n.bus, bus.TopicACPPermissionResolved, "acp-relay", toACPEvent(p, decision, reason))
}

// toACPEvent maps the internal/acp snapshot to the frozen bus payload
// (keys: id, agent_id, session_id, summary, created_at + decision/reason).
func toACPEvent(p acp.PendingPermission, decision, reason string) bus.ACPPermissionEvent {
	created := ""
	if !p.CreatedAt.IsZero() {
		created = p.CreatedAt.UTC().Format(time.RFC3339)
	}
	return bus.ACPPermissionEvent{
		ID:        p.ID,
		AgentID:   p.AgentID,
		SessionID: p.SessionID,
		Summary:   p.Summary,
		CreatedAt: created,
		Decision:  decision,
		Reason:    reason,
	}
}
