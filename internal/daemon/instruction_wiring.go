package daemon

import (
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/tools"
)

// instructionWiringResult holds the constructed instruction-system components
// returned by wireInstructions for later attachment to Components.
type instructionWiringResult struct {
	Store           *preferences.Store
	Handler         *agent.InstructionHandler
	Listener        *agent.InstructionListener
	Scheduler       *scheduler.InstructionScheduler
	ContextInjector *agent.ContextInjector
	Parser          *agent.InstructionParser
	Verifier        *preferences.InstructionVerifier
}

// wireInstructions constructs the user-instruction subsystem:
//   - UserInstructionStore (tiered YAML discovery)
//   - InstructionParser + InstructionVerifier (parse/validate NL instructions)
//   - InstructionHandler (bus subscriptions for add/list/delete/execute/preview)
//   - InstructionListener (post_hook and event trigger matching)
//   - ContextInjector (merges standing instructions + learned patterns into
//     the system prompt)
//
// The InstructionScheduler is NOT constructed here: the daemon job
// scheduler does not exist yet at this point in NewComponents (it is
// built ~2,200 lines later, gated on cfg.Scheduler.Enabled), and the old
// single-function shape built it behind a nil guard that failed on EVERY
// boot -- cron-type user instructions were silently never scheduled
// (07-bench/09 audit F2). Construction now lives in
// wireInstructionScheduler, called right after the daemon scheduler is
// assigned.
//
// The handler and listener are NOT started here; the caller must invoke
// Start on them during the daemon Start lifecycle (see Components.Start).
// All components are nil-safe: missing dependencies result in a no-op
// rather than a panic.
//
// The returned result allows the caller to attach the store and context
// injector to the agent loop and other downstream consumers.
func wireInstructions(
	msgBus *bus.MessageBus,
	daemonScheduler *scheduler.Scheduler,
	toolRegistry *tools.Registry,
	learningPipeline *selfimprove.LearningPipeline,
	logger *slog.Logger,
) instructionWiringResult {
	result := instructionWiringResult{}

	if msgBus == nil {
		logger.Warn("instruction wiring skipped: no message bus")
		return result
	}

	// 1. Construct the tiered instruction store.
	store := preferences.NewUserInstructionStore(preferences.DefaultTiers)
	if _, discErr := store.Discovery(); discErr != nil {
		logger.Warn("instruction store discovery failed", "error", discErr)
	}
	result.Store = store

	// 2. Construct the NL instruction parser.
	parser := agent.NewInstructionParser()

	// 3. Construct the verifier. A nil tool registry is acceptable at boot
	//    time -- the verifier will warn on unknown tools but will not reject
	//    them outright.
	verifier := preferences.NewInstructionVerifier(toolRegistry)
	result.Parser = parser
	result.Verifier = verifier

	// 4. Construct the instruction handler (bus subscriptions for
	//    instruction.add, instruction.list, instruction.delete,
	//    instruction.execute, instruction.preview). Not started here;
	//    the caller starts it during the daemon Start lifecycle.
	handler := agent.NewInstructionHandler(store, msgBus, parser, verifier, logger.With("component", "instruction-handler"))
	result.Handler = handler

	// 5. Construct the instruction listener (post_hook + event trigger
	//    matching). A nil tool executor is acceptable -- the listener
	//    currently logs actions rather than executing them.
	listener := agent.NewInstructionListener(store, msgBus, nil, logger.With("component", "instruction-listener"))
	result.Listener = listener

	// 6. The instruction scheduler (cron-type instructions) is constructed
	//    later by wireInstructionScheduler, after the daemon scheduler
	//    exists. daemonScheduler is retained in the signature for callers
	//    that hold one already (e.g. tests) but is deliberately unused
	//    here.
	_ = daemonScheduler

	// 7. Construct the ContextInjector (merges standing instructions and
	//    learned patterns into the system prompt).
	result.ContextInjector = agent.NewContextInjector(learningPipeline, store)
	logger.Info("instruction subsystem constructed",
		"has_scheduler", result.Scheduler != nil,
		"has_injector", result.ContextInjector != nil,
	)

	return result
}

// wireInstructionScheduler constructs the InstructionScheduler (cron-type
// user instructions synced onto the daemon job scheduler). It must run
// AFTER c.Scheduler is assigned in NewComponents -- calling it earlier is
// the exact F2 bug shape (nil scheduler => silently no instruction cron
// scheduling for the whole boot). No-op with a debug log when the daemon
// scheduler is absent (cfg.Scheduler.Enabled=false) or the instruction
// store was not wired (msgBus was nil at the early wiring site).
func (c *Components) wireInstructionScheduler(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if c.Scheduler == nil {
		logger.Debug("instruction scheduler skipped: no daemon scheduler")
		return
	}
	if c.InstructionStore == nil {
		logger.Warn("instruction scheduler skipped: instruction store not wired")
		return
	}
	c.InstructionScheduler = scheduler.NewInstructionScheduler(c.Scheduler, c.InstructionStore, logger.With("component", "instruction-scheduler"))
	logger.Info("instruction scheduler constructed")
}
