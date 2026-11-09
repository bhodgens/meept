package daemon

// delegate_rpc.go wires the delegate.status RPC surface (DSH-9d, audit
// item 5 operator half) onto the live C1 DelegateManager. Pattern:
// registerVoiceLocalRPC / registerIntakeStatusRPC — a nil-safe registration
// helper called from the daemon's RPC registration section; a daemon with
// no delegate wiring still answers (the handler's disabled envelope),
// never "method not found". Report-only: the surface exposes no write
// primitive — child control stays with the model-facing subagent_* tools.

import (
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/rpc"
)

// registerDelegateStatusRPC registers delegate.status on the RPC server.
// Nil-safe at every layer.
func registerDelegateStatusRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	var seam rpc.DelegateChildrenSeam
	if c != nil && c.DelegateManager != nil {
		dm := c.DelegateManager
		seam.Children = func() []rpc.DelegateChildRow {
			return delegateChildRows(dm.AllChildren())
		}
	}

	handler := rpc.NewDelegateChildrenHandler(seam)
	handler.RegisterDelegateStatusMethods(server)
	if seam.Children != nil {
		logger.Info("delegate.status RPC handler registered")
	} else {
		logger.Info("delegate.status RPC handler registered (delegate registry not wired: empty reports)")
	}
}

// delegateChildRows projects child records onto the wire rows (sanitized:
// result presence only, error text bounded; no seed, no prompt, no result
// payload).
func delegateChildRows(records []agent.ChildRecord) []rpc.DelegateChildRow {
	rows := make([]rpc.DelegateChildRow, 0, len(records))
	for _, rec := range records {
		mode := rec.Mode
		if mode == "" {
			mode = "spawn"
		}
		createdAt := ""
		if !rec.CreatedAt.IsZero() {
			createdAt = rec.CreatedAt.Format(time.RFC3339)
		}
		rows = append(rows, rpc.NewDelegateChildRow(
			rec.ChildID, rec.AgentID, rec.ParentSessionID,
			mode, string(rec.State), rec.Model, rec.Provider,
			createdAt, rec.Result != "", rec.Err,
		))
	}
	return rows
}
