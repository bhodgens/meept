package builtin

import (
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
)

// schedule_register.go populates the toolprov provider seam for the
// scheduler/cron tool group (contract C1,
// docs/plans/20261031-tool-provider-seam/03-migrate.md T1).
//
// The provider's construction input (the live *scheduler.Scheduler) is
// a runtime value only the daemon can supply - the scheduler is nil at
// the initial registerBuiltinTools call and created afterwards - so
// this registers a provider FACTORY whose handle is captured at
// Discover time via SetSchedulerProvider, the same pattern
// github_register.go established for the github provider. An unbound
// factory fails closed: toolprov.RegisterAll rejects a nil Provider
// with a descriptive error instead of registering tools that cannot
// reach a scheduler.
//
// The provider contributes the SAME eight tools at the SAME registry
// names the former direct Register calls at the daemon wiring site
// built (schedule_create/list/get/delete/pause/resume/run_now +
// cron_create), all constructors called with the same argument
// (c.Scheduler), so names, categories, and behaviors are identical to
// the pre-seam registration path.
func init() {
	if err := toolprov.Register("scheduler", schedulerProviderFactory); err != nil {
		panic("builtin: scheduler provider registration: " + err.Error())
	}
}

// schedulerProviderFactoryFunc builds the scheduler provider from the
// handle a Discover-time caller captures. Declared as a variable so the
// daemon can bind the live scheduler before toolprov.RegisterAll
// constructs the provider.
var schedulerProviderFactoryFunc func() toolprov.Provider

// schedulerProviderFactory is the thunk handed to toolprov.Register.
// It defers to schedulerProviderFactoryFunc; an unbound factory returns
// nil and toolprov.RegisterAll fails closed on the nil Provider.
func schedulerProviderFactory() toolprov.Provider {
	if schedulerProviderFactoryFunc == nil {
		return nil
	}
	return schedulerProviderFactoryFunc()
}

// SetSchedulerProvider binds the scheduler provider's construction
// input. The broker binding is the in-process secrets broker (schedule
// tools hold no secret material today; the binding is the uniform
// two-lane seam).
func SetSchedulerProvider(sched *scheduler.Scheduler, broker *secrets.Broker) {
	schedulerProviderFactoryFunc = func() toolprov.Provider {
		return &scheduleProvider{sched: sched, broker: broker}
	}
}

// scheduleProvider is the toolprov.Provider for the scheduler/cron
// tool group.
type scheduleProvider struct {
	sched  *scheduler.Scheduler
	broker *secrets.Broker
}

// Name implements toolprov.Provider.
func (p *scheduleProvider) Name() string { return "scheduler" }

// Discover implements toolprov.Provider. It mirrors the former direct
// registration block exactly: same tools, same order, same constructor
// arguments.
func (p *scheduleProvider) Discover() []tools.Tool {
	return []tools.Tool{
		NewScheduleCreateTool(p.sched),
		NewScheduleListTool(p.sched),
		NewScheduleGetTool(p.sched),
		NewScheduleDeleteTool(p.sched),
		NewSchedulePauseTool(p.sched),
		NewScheduleResumeTool(p.sched),
		NewScheduleRunNowTool(p.sched),
		NewCronCreateTool(p.sched),
	}
}

// PermissionClass implements toolprov.Provider. The scheduler group is
// mutating (it creates, deletes, pauses, resumes, and force-runs
// scheduled jobs); it never spawns processes, so it is not privileged.
func (p *scheduleProvider) PermissionClass() toolprov.RiskClass {
	return toolprov.RiskMutating
}

// Broker implements toolprov.Provider.
func (p *scheduleProvider) Broker() *secrets.Broker {
	return p.broker
}
