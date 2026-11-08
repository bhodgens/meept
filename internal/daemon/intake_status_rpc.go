package daemon

// intake_status_rpc.go registers the intake.status RPC surface (issue #109
// TUI/GUI parity). The TUI half (9a19d0be) shipped the rpc.IntakeStatusHandler
// with daemon-side wiring as the stated follow-up; the GUI's HTTP bridge
// (GET /api/v1/intake/status -> s.rpcCall -> intake.status) needs the method
// registered on the live server, else the bridge would surface
// "method not found" on every poll. This is pure wiring of the existing
// handler — no new behaviour, report-only, presence-only (no token material
// ever crosses the wire; the GitHub token presence lives in `meept github
// status` / the [github] block).

import (
	"log/slog"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/rpc"
)

// registerIntakeStatusRPC registers intake.status on the RPC server.
// Nil-safe: no server -> no-op; no components/config -> the handler's
// disabled-envelope posture (the nil config fn is allowed by
// NewIntakeStatusHandler; a daemon that never wired [intake] still
// answers, never "method not found"). Pattern: wireActivityComponents
// (a wiring helper called from daemon Run before the listeners serve).
func registerIntakeStatusRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Config snapshot fn: reads Components.Config fresh on every call so a
	// config swap (reload path) is reflected without re-registration. The
	// fields ride as-is; client surfaces (TUI renderIntakeStatus, GUI)
	// render the shipped defaults for empty values — same frozen contract
	// as rpc.IntakeStatusView's doc comment.
	var configFn func() rpc.IntakeStatusView
	if c != nil {
		comp := c
		configFn = func() rpc.IntakeStatusView {
			if comp.Config == nil {
				return rpc.IntakeStatusView{}
			}
			return intakeStatusViewFromConfig(comp.Config.Intake)
		}
	}

	// Poller-scheduled probe: the live scheduler's job table, keyed on the
	// same job name wireIntakePoller schedules ("intake_poller"). Nil
	// scheduler -> not scheduled.
	var schedFn func() bool
	if c != nil {
		comp := c
		schedFn = func() bool {
			if comp.Scheduler == nil {
				return false
			}
			_, ok := comp.Scheduler.GetJob(intakePollJobName)
			return ok
		}
	}

	handler := rpc.NewIntakeStatusHandler(configFn, schedFn)
	handler.RegisterIntakeStatusMethods(server)
	logger.Info("intake.status RPC handler registered")
}

// intakeStatusViewFromConfig maps the [intake] config block onto the
// frozen cross-surface view. Defaults live in the client renderers, not
// here: the view mirrors exactly what the operator configured.
func intakeStatusViewFromConfig(cfg config.IntakeConfig) rpc.IntakeStatusView {
	return rpc.IntakeStatusView{
		Enabled:         cfg.Enabled,
		WatchRepo:       cfg.WatchRepo,
		PollInterval:    cfg.PollInterval,
		RequiredLabels:  cfg.RequiredLabels,
		DoneLabel:       cfg.DoneLabel,
		DefaultEmployee: cfg.DefaultEmployee,
		GateCommand:     cfg.GateCommand,
		ProjectNumber:   cfg.ProjectNumber,
	}
}
