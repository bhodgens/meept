package daemon

import (
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
)

// wireAgentLoopManager constructs the per-session AgentLoop manager and
// its backing worker pool, wiring both onto Components.
//
// The singleton c.AgentLoop is left untouched — it remains the default
// loop for non-session-scoped call sites (daemon endpoints, telegram,
// watchdog). The manager provides session-scoped loops on demand via
// GetOrCreate.
//
// No-op when cfg.Agent.WorkerPool.Enabled is false.
func wireAgentLoopManager(c *Components, cfg *config.Config, logger *slog.Logger) {
	wpCfg := cfg.Agent.WorkerPool

	// The AgentLoopManager is required for per-session project isolation.
	// Without it, all sessions share a single singleton loop and overwrite
	// each other's workingDir. Always create the manager, even when the
	// worker pool is disabled.
	if wpCfg.Enabled {
		// Build WorkerPoolConfig from user config, falling back to defaults
		// for zero-value fields.
		poolCfg := DefaultWorkerPoolConfig()
		if wpCfg.MaxWorkers > 0 {
			poolCfg.MaxWorkers = wpCfg.MaxWorkers
		}
		if wpCfg.MaxLoopsPerWorker > 0 {
			poolCfg.MaxLoopsPerWorker = wpCfg.MaxLoopsPerWorker
		}
		if wpCfg.IdleTimeout != "" {
			if d, err := time.ParseDuration(wpCfg.IdleTimeout); err == nil {
				poolCfg.IdleTimeout = d
			} else {
				logger.Warn("invalid agent.worker_pool.idle_timeout; using default",
					"value", wpCfg.IdleTimeout,
					"default", poolCfg.IdleTimeout,
					"error", err,
				)
			}
		}

		pool := NewWorkerPool(poolCfg)
		pool.Start()
		c.AgentWorkerPool = pool

		mgr := agent.NewManager(agent.ManagerConfig{
			SessionStore: c.SessionStore,
			ProjectMgr:   c.ProjectManager,
		})
		c.AgentLoopManager = mgr

		logger.Info("agent loop manager wired (with worker pool)",
			"max_workers", poolCfg.MaxWorkers,
			"max_loops_per_worker", poolCfg.MaxLoopsPerWorker,
			"idle_timeout", poolCfg.IdleTimeout,
			"has_session_store", c.SessionStore != nil,
			"has_project_manager", c.ProjectManager != nil,
		)
	} else {
		// Worker pool disabled, but still create the manager so sessions
		// get isolated loops with correct working directories.
		mgr := agent.NewManager(agent.ManagerConfig{
			SessionStore: c.SessionStore,
			ProjectMgr:   c.ProjectManager,
		})
		c.AgentLoopManager = mgr

		logger.Info("agent loop manager wired (no worker pool)",
			"has_session_store", c.SessionStore != nil,
			"has_project_manager", c.ProjectManager != nil,
		)
	}
}
