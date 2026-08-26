// lifecycle.go implements graceful shutdown drain
// (loop-economics/06-doctor-lifecycle): stop accepting new jobs -> drain
// running jobs up to the drain timeout -> close listeners -> persist state.
package daemon

import (
	"context"
	"time"
)

// DefaultDrainTimeout is the default grace period for running jobs (30s).
const DefaultDrainTimeout = 30 * time.Second

// RunningJobCounter reports how many jobs are still running. Returning 0
// means the drain is complete.
type RunningJobCounter func(ctx context.Context) (int, error)

// DrainRunningJobs polls count until it reports zero, the deadline expires,
// or ctx is cancelled — whichever comes first. It never returns an error:
// shutdown must proceed regardless of drain outcome.
func DrainRunningJobs(ctx context.Context, count RunningJobCounter, timeout time.Duration) {
	if timeout <= 0 {
		timeout = DefaultDrainTimeout
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		n, err := count(ctx)
		if err == nil && n <= 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return // deadline hit: remaining jobs are abandoned/killed by callers
		case <-tick.C:
		}
	}
}
