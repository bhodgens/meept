package worker

import "github.com/caimlas/meept/internal/queue"

// SetQueue swaps the queue that FUTURE workers claim from. Cluster wiring
// uses this to move the pool from the raw PersistentQueue onto the
// ClusterQueue (claim bookkeeping + stale-claim reclaim), which is wired
// after NewComponents creates the pool (daemon.go ordering).
//
// Live workers intentionally keep their existing queue handle: their wake
// channels remain valid because ClusterQueue does not override Enqueue - it
// forwards to the same wrapped PersistentQueue, so wake signals keep firing
// on the old registrations. In-flight claims drain through the old handle
// against the same underlying store. New claims (worker churn via
// AddWorker/Scale) pick up the ClusterQueue immediately.
func (p *Pool) SetQueue(q queue.Queue) {
	if q == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.queue == nil || p.queue == q {
		return
	}
	p.queue = q
}
