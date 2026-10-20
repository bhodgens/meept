package daemon

import (
	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// applyACPFromConfig constructs the ACP manager when [acp] enabled=true.
// Disabled (the DefaultConfig path) returns (nil, nil) with no log line:
// ACP is opt-in and a disabled manager is a valid construction result, not
// an error — callers nil-check the manager.
//
// dec is the permission lane's decision seam (acp-permission-decision leaf
// 03, Contract C). It is applied ONLY when cfg.PermissionMode == "decision";
// permissive/deny managers stay byte-identical with no decider attached. A
// nil dec in decision mode is legal at this layer — the manager keeps no
// client and sessions fail closed with no_decisioner (pinned in
// internal/acp). This function stays log-free by contract (the zero-log
// disabled path is pinned by acp_wiring_test.go); the caller emits the
// single "acp permission decision lane armed" line.
//
// msgBus (variadic so leaf 01/02-era 2-arg callers stay valid; production
// passes it) arms the human relay seam (acp-human-permission-relay leaf 03,
// Contract C): mode "human", or mode "decision" with
// decision_escalate_to_human, builds the registry + bus notifier via
// SetPermissionRelay. Without a bus the relay stays unwired and every lane
// fails closed exactly like the pre-leaf-03 build (pinned by
// TestApplyACPFromConfig_HumanModeNilBusFailsClosed).
func applyACPFromConfig(cfg config.ACPConfig, dec acp.Decisioner, msgBus ...*bus.MessageBus) (*acp.Manager, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // disabled ACP is an intentional "not present" contract, not an error
	}
	mgr, err := acp.NewManagerFromFiles(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.PermissionMode == "decision" && dec != nil {
		mgr.SetDecisionClient(dec)
	}
	var busRef *bus.MessageBus
	if len(msgBus) > 0 {
		busRef = msgBus[0]
	}
	if busRef != nil &&
		(cfg.PermissionMode == "human" ||
			(cfg.PermissionMode == "decision" && cfg.DecisionEscalateToHuman)) {
		mgr.SetPermissionRelay(acp.NewRelay(), &acpBusNotifier{bus: busRef, logger: nil})
	}
	return mgr, nil
}

// RegisterACPTools registers the acp_agent builtin when the manager is live.
// No-op for a nil registry, nil manager, or disabled manager.
func RegisterACPTools(reg *tools.Registry, mgr *acp.Manager) {
	if mgr == nil || !mgr.Enabled() || reg == nil {
		return
	}
	reg.Register(builtin.NewACPAgentTool(mgr))
}
