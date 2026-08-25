package tui

import (
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/modals"
)

// changesRPCAPI implements modals.PendingChangeAPI over the daemon's
// changes.* RPC methods. The RPC handlers share the daemon-side accept
// path (ResolveTool.AcceptChange) with the HTTP endpoints, so the TUI
// modal and the HTTP/Flutter surfaces resolve changes identically.
type changesRPCAPI struct {
	rpc *RPCClient
}

// ListPendingChanges fetches the staged changes for one session.
func (c *changesRPCAPI) ListPendingChanges(sessionID string) ([]modals.PendingChange, error) {
	if c.rpc == nil || !c.rpc.IsConnected() {
		return nil, fmt.Errorf("not connected to daemon")
	}
	result, err := c.rpc.Call("changes.list", map[string]string{ParamSessionID: sessionID})
	if err != nil {
		return nil, fmt.Errorf("changes.list: %w", err)
	}
	var resp struct {
		Changes []modals.PendingChange `json:"changes"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("changes.list: decode response: %w", err)
	}
	return resp.Changes, nil
}

// AcceptPendingChange applies one staged change.
func (c *changesRPCAPI) AcceptPendingChange(id string) error {
	if c.rpc == nil || !c.rpc.IsConnected() {
		return fmt.Errorf("not connected to daemon")
	}
	if _, err := c.rpc.Call("changes.accept", map[string]string{"id": id}); err != nil {
		return fmt.Errorf("changes.accept: %w", err)
	}
	return nil
}

// RejectPendingChange discards one staged change.
func (c *changesRPCAPI) RejectPendingChange(id string) error {
	if c.rpc == nil || !c.rpc.IsConnected() {
		return fmt.Errorf("not connected to daemon")
	}
	if _, err := c.rpc.Call("changes.reject", map[string]string{"id": id}); err != nil {
		return fmt.Errorf("changes.reject: %w", err)
	}
	return nil
}

// pendingChangesTickMsg triggers a periodic status-bar count refresh.
type pendingChangesTickMsg struct{}

// pendingChangesTick schedules the next count refresh. Every ticks in sync
// with the system clock, so overlapping ticks coalesce.
func (a *App) pendingChangesTick() tea.Cmd {
	return tea.Every(10*time.Second, func(_ time.Time) tea.Msg {
		return pendingChangesTickMsg{}
	})
}

// refreshPendingChangesCount fetches the pending-change count for the
// current session for the status bar indicator.
func (a *App) refreshPendingChangesCount() tea.Cmd {
	if a.pendingChangesModal == nil || a.currentSession == nil {
		return nil
	}
	return a.pendingChangesModal.FetchCount(a.currentSession.ID)
}

// pastTenseChangeAction maps a modal action to its lowercase past tense for
// status bar messages ("accepted" / "rejected").
func pastTenseChangeAction(action string) string {
	switch action {
	case "accept":
		return "accepted"
	case "reject":
		return "rejected"
	default:
		return action + "ed"
	}
}
