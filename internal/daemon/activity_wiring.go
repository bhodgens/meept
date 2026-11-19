package daemon

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/rpc"
)

// activityRingCapacity is the live-tail ring size (master.md Q3 default:
// 1000 events, ~1MB).
const activityRingCapacity = 1000

// wireActivityComponents constructs the activity Aggregator + Ring over the
// Components handles and the plan resolver from PlanManager, stores them on
// Components, subscribes the ring bus bridge, and registers the activity +
// drift RPC surface. Call AFTER all components exist and BEFORE the RPC
// server starts serving (pattern: otlp_wiring.go - a wiring helper file).
//
// Nil-safe at every step: a missing handle (task registry, session store,
// audit chain, employee stack, plan manager) simply degrades the affected
// method to a clean RPC error at call time - wiring never fails boot.
// Double registration (a method name already owned by another surface)
// logs and skips inside registerActivityRPC.
func wireActivityComponents(c *Components, msgBus *bus.MessageBus, server *rpc.Server, logger *slog.Logger) {
	if c == nil || server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Plan resolver: inverted linkage (plans.task_id -> tasks.id) walked
	// through the PlanManager read path. Nil PlanManager degrades plan
	// stamping to records' own plan ids.
	var planRes activity.PlanResolver
	if c.PlanManager != nil {
		pm := c.PlanManager
		planRes = func(ctx context.Context, planID string) ([]string, error) {
			p, err := pm.GetPlan(ctx, planID)
			if err != nil {
				return nil, err
			}
			if p == nil || p.TaskID == "" {
				return nil, nil
			}
			return []string{p.TaskID}, nil
		}
	}

	// Audit chain: the employee wiring hands the Manager the chain DB
	// handle; the aggregator reads it through an auditlog store opened
	// over the same pool (OpenStoreFromDB's Close is a no-op on
	// caller-owned handles, matching the shared-DB convention). Nil chain
	// (chaining disabled) degrades to no audit_record kind.
	var chain *auditlog.Store
	if chainDB := activityChainDB(c); chainDB != nil {
		if s, err := auditlog.OpenStoreFromDB(chainDB, logger); err == nil {
			chain = s
		} else {
			logger.Warn("activity wiring: audit chain store unavailable; audit_record kind degraded",
				"error", err)
		}
	}

	agg := activity.NewAggregator(
		c.TaskRegistry,
		c.StepStore,
		c.SessionStore,
		chain,
		c.EmployeeAuditStore,
		activity.Options{PlanResolver: planRes},
	)

	ring := activity.NewRing(activityRingCapacity)
	if msgBus != nil {
		// Live tail: bus events (queue.job.*, task.completed/failed,
		// agent.progress, turn.terminal) map to ActivityRecord and append
		// to the ring. Best-effort, never blocks publishers (contract C3).
		// The returned subscriptions are retained so stopComponents can
		// unsubscribe them (goroutine teardown, not process-exit leak).
		c.activityBridgeSubs = activity.WireBusBridge(msgBus, ring)
	}

	c.ActivityAggregator = agg
	c.ActivityRing = ring

	registerActivityRPC(server, agg, ring, c.EmployeeManager, planRes)
	logger.Info("activity RPC handlers registered",
		"ring_capacity", activityRingCapacity,
		"chain_wired", chain != nil)

	// Durable execution-run history (issue #160 follow-up): a SQLite
	// projection of bot.result / employee.result so GUI/TUI can page runs
	// after reconnect AND daemon restart. Store open failure degrades the
	// agents.executionRuns surface to a clean RPC error; boot continues.
	if store, err := activity.OpenExecutionStore(config.MeeptPath("execution_runs.db"), logger); err != nil {
		logger.Warn("execution store unavailable; agents.executionRuns degraded", "error", err)
	} else {
		c.ExecutionStore = store
		if msgBus != nil {
			c.executionProjectorSubs = activity.WireExecutionProjector(msgBus, store, logger)
		}
		registerExecutionRunsRPC(server, store)
		logger.Info("execution-run history store wired", "path", config.MeeptPath("execution_runs.db"))
	}
}

// activityChainDB extracts the audit-chain DB handle nil-safely (typed-nil
// rule: the Manager method is called only behind a non-nil Manager check).
func activityChainDB(c *Components) *sql.DB {
	if c == nil || c.EmployeeManager == nil {
		return nil
	}
	return c.EmployeeManager.AuditChain()
}

// stopActivityRing drops the ring reference and unsubscribes the live-tail
// bus bridge on shutdown. Unsubscribing closes each subscriber channel, so
// the bridge's reader goroutines exit instead of leaking across in-process
// daemon restarts.
func stopActivityRing(c *Components, msgBus *bus.MessageBus) {
	if c == nil {
		return
	}
	if msgBus != nil {
		for _, sub := range c.activityBridgeSubs {
			if sub != nil {
				msgBus.Unsubscribe(sub)
			}
		}
	}
	c.activityBridgeSubs = nil
	c.ActivityRing = nil

	// Execution-run history: unsubscribe the projector readers and close
	// the SQLite handle so in-process restarts neither leak goroutines nor
	// hold the WAL file open.
	if msgBus != nil {
		for _, sub := range c.executionProjectorSubs {
			if sub != nil {
				msgBus.Unsubscribe(sub)
			}
		}
	}
	c.executionProjectorSubs = nil
	if c.ExecutionStore != nil {
		if err := c.ExecutionStore.Close(); err != nil {
			slog.Warn("execution store: close failed", "error", err)
		}
		c.ExecutionStore = nil
	}
}
