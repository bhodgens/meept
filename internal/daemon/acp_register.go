package daemon

import (
	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// applyACPFromConfig constructs the ACP manager when [acp] enabled=true.
// Disabled (the DefaultConfig path) returns nil, nil with no log line.
func applyACPFromConfig(cfg config.ACPConfig) (*acp.Manager, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	return acp.NewManagerFromFiles(cfg)
}

// RegisterACPTools registers the acp_agent builtin when the manager is live.
// No-op for a nil registry, nil manager, or disabled manager.
func RegisterACPTools(reg *tools.Registry, mgr *acp.Manager) {
	if mgr == nil || !mgr.Enabled() || reg == nil {
		return
	}
	reg.Register(builtin.NewACPAgentTool(mgr))
}
