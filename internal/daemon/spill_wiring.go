package daemon

import (
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Tool-spill sweep constants (token-efficiency branch 01, leaf 02; plan C2).
// The sweep is pure (llm.SweepToolSpills): a startup pass and a periodic
// pass that keep $MEEPT_HOME/spill/tools bounded by age AND total size.
const (
	// spillSweepAgeCutoff is how old a tool-spill file may get before the
	// age pass removes it.
	spillSweepAgeCutoff = 24 * time.Hour
	// spillSweepSizeCap bounds the total bytes of the tools spill tree;
	// the size pass evicts oldest-first until under this cap.
	spillSweepSizeCap = int64(512) << 20 // 512 MiB
	// spillSweepTickInterval is the periodic sweep interval.
	spillSweepTickInterval = 6 * time.Hour
)

// runToolSpillSweep executes one sweep pass over the default tools spill
// root. The removed count is logged at info (a zero sweep stays Debug so
// steady-state daemons do not spam the log); errors are logged and NEVER
// returned to a fatal path — a broken spill tree must not take the daemon
// down (sweep errors are startup-recoverable, not platform failures).
func (c *Components) runToolSpillSweep() {
	root := llm.DefaultToolSpillRoot()
	removed, err := llm.SweepToolSpills(root, spillSweepAgeCutoff, spillSweepSizeCap, time.Now())
	if err != nil {
		c.Logger.Warn("tool-spill sweep failed", "root", root, "error", err)
		return
	}
	if removed > 0 {
		c.Logger.Info("tool-spill sweep complete",
			"root", root, "removed_files", removed,
			"age_cutoff", spillSweepAgeCutoff.String(),
			"size_cap_bytes", spillSweepSizeCap)
	} else {
		c.Logger.Debug("tool-spill sweep complete", "root", root, "removed_files", 0)
	}
}

// startToolSpillSweep runs the startup sweep pass BEFORE any spawn
// (mirroring the orphan-sweep "sweep before spawn" rule in
// internal/daemon/AGENTS.md — cheap file I/O, never blocks readiness
// meaningfully) and then arms a 6h ticker that stops cleanly when the
// components lifecycle context is cancelled — no goroutine leak on
// shutdown.
//
// Following the github re-arm ticker pattern: a done channel is captured
// locally (deferred close can never reach a replaced field), and the tick
// body is panic-recovered so one bad tick cannot kill the daemon.
func (c *Components) startToolSpillSweep() {
	// Startup pass, synchronous: log only, never fatal.
	c.runToolSpillSweep()

	done := make(chan struct{})
	c.ToolSpillTickerDone = done
	ticker := time.NewTicker(spillSweepTickInterval)
	go func() { //nolint:gosec // daemon-lifetime goroutine, exits via c.ctx like its siblings
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// recover from panic in one tick so a panic cannot kill the daemon
				func() {
					defer func() {
						if r := recover(); r != nil {
							c.Logger.Error("tool-spill ticker panic recovered", "recovered", r)
						}
					}()
					c.runToolSpillSweep()
				}()
			case <-c.ctx.Done():
				return
			}
		}
	}()
	c.Logger.Info("tool-spill sweep ticker started",
		"interval_seconds", int(spillSweepTickInterval.Seconds()))
}

// stopToolSpillSweep waits for the ticker goroutine to exit after the
// lifecycle context cancellation in stopComponents. Safe to call when it
// was never started. The done channel is NEVER nil'd (a nil channel in a
// select blocks forever — the channel-nilafterclose regression class this
// repo's hook guards against).
func (c *Components) stopToolSpillSweep() {
	if c.ToolSpillTickerDone == nil {
		return
	}
	ticker := time.NewTimer(5 * time.Second)
	defer ticker.Stop()
	select {
	case <-c.ToolSpillTickerDone:
	case <-ticker.C:
		c.Logger.Warn("tool-spill sweep ticker did not stop within 5s")
	}
}
