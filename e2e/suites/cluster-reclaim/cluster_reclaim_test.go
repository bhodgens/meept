//go:build e2e

// Package clusterreclaim covers the multi-node queue claim path at the
// daemon boundary (manifest scenarios cluster-reclaim-01..03): two scratch
// daemons share one queue.db (the cluster store-sharing design), each runs
// its own ClusterQueue sweeper, and an expired remote claim is reclaimed
// by the survivor's sweeper.
//
// Claim primitive: the queue.claim RPC against a cluster-wired daemon. The
// handler's queue is swapped onto the ClusterQueue by the wiring, so an RPC
// claim is a REAL clustered claim (stamps claimed_by_node + timeout_at).
//
// Worker-pool note: pool workers claim any pending job (empty agentID =
// no filter), so a seeded pending row may be processed by a worker before
// the test observes it. Scenarios 02/03 therefore seed rows ALREADY in the
// claimed state with explicit cluster stamps - the exact input the remote
// sweeper consumes - making the window deterministic. Scenario 01 uses the
// RPC race intentionally: it asserts the stamp appears however the claim
// lands (RPC or worker), which is the production guarantee.
//
// What is NOT under test here: gossip wire transport and WireGuard mesh
// (both need privileged loopback aliases / a second host - covered by the
// manual live-verification procedure in docs/workflows/cluster.md). The
// reclaim path itself is store-driven: the sweeper reads claimed rows from
// the shared DB, so exercising it needs exactly the two-daemon shape this
// suite boots.
package clusterreclaim

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

const sweepDeadline = 20 * time.Second

// startNode boots one scratch daemon as a cluster node: own MEEPT_HOME, node
// ID, gossip listener (bound but never dialed - the reclaim path is
// store-driven), and queue db_path pointed at sharedQueue.
func startNode(t *testing.T, nodeID, sharedQueue, gossipAddr string) *harness.Stack {
	t.Helper()
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"orchestrator": map[string]any{"classifier_boot_fail_fast": false},
			"queue":        map[string]any{"db_path": sharedQueue},
		}),
		harness.WithPreBootHook(func(stack *harness.Stack) error {
			cfg := fmt.Sprintf(`{
  "cluster_id": "e2e-reclaim",
  "cluster_name": "e2e-reclaim",
  "join_key": "e2e-test-key",
  "node_id": %q,
  "network": {
    "wireguard_port": 0,
    "gossip_listen_addr": %q
  },
  "gossip": {
    "heartbeat_interval": "1s",
    "peer_timeout": "10s"
  },
  "queue": {
    "default_claim_timeout": "2s",
    "node_reachability_timeout": "10s"
  },
  "git": { "sync_interval": "0", "heartbeat_commit": false, "remote_url": "" }
}`, nodeID, gossipAddr)
			return writeFile(filepath.Join(stack.MeeptHome, "cluster", "config.json5"), cfg)
		}),
	)
	return s
}

// openShared opens the shared queue DB for direct row reads/writes.
func openShared(t *testing.T, sharedQueue string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sharedQueue+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open shared queue db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedClaimed inserts a job already in the claimed state, stamped as if
// claimed by ownerNode at seed time with the given timeout. This is the
// exact row shape ClusterQueue.Claim produces (store_cluster_claim.go) and
// the exact input the remote sweeper scans for.
func seedClaimed(t *testing.T, db *sql.DB, jobID, ownerNode string, timeout time.Duration) {
	t.Helper()
	now := time.Now().UTC()
	_, err := db.Exec(`INSERT INTO jobs (id, type, priority, state, payload, required_caps,
			max_retries, created_at, updated_at, claimed_by, claimed_by_node, claim_token, timeout_at, last_heartbeat_at)
		VALUES (?, 'one_off', 2, 'claimed', '{"prompt":"e2e cluster reclaim"}', '[]', 3, ?, ?,
			'e2e-worker', ?, 'claim-e2e-token', ?, ?)`,
		jobID, now.Format(time.RFC3339), now.Format(time.RFC3339),
		ownerNode, now.Add(timeout).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("seed claimed job %s: %v", jobID, err)
	}
}

// jobRow reads the cluster-relevant columns of one job from the shared DB.
func jobRow(t *testing.T, db *sql.DB, jobID string) (state, claimedByNode string) {
	t.Helper()
	err := db.QueryRow(`SELECT COALESCE(state,''), COALESCE(claimed_by_node,'')
		FROM jobs WHERE id = ?`, jobID).Scan(&state, &claimedByNode)
	if err != nil {
		t.Fatalf("query job %s: %v", jobID, err)
	}
	return
}

// waitFor polls pred until it holds or the deadline passes.
func waitFor(t *testing.T, deadline time.Duration, what string, pred func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if pred() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for: %s", deadline, what)
}

// TestClusterReclaim01_ClaimStampsOwnership: a claim landing on a
// cluster-wired daemon (here: the queue.claim RPC racing the pool's
// workers - either winner proves it) stamps claimed_by_node + timeout_at
// on the row. The stamps are the remote sweeper's entire input; they were
// never written on the claim path before the wiring.
func TestClusterReclaim01_ClaimStampsOwnership(t *testing.T) {
	gossipA := "127.0.0.1:" + strconv.Itoa(harness.FreePort(t))
	sharedQueue := filepath.Join(t.TempDir(), "shared-queue.db")
	nodeA := startNode(t, "node-a", sharedQueue, gossipA)

	db := openShared(t, sharedQueue)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO jobs (id, type, priority, state, payload, required_caps,
			max_retries, created_at, updated_at)
		VALUES ('reclaim-01-job', 'one_off', 2, 'pending', '{"prompt":"x"}', '[]', 3, ?, ?)`,
		now, now); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	waitFor(t, sweepDeadline, "claim stamp (claimed_by_node + timeout_at) on the row", func() bool {
		var node, timeoutAt string
		err := db.QueryRow(`SELECT COALESCE(claimed_by_node,''), COALESCE(timeout_at,'')
			FROM jobs WHERE id = 'reclaim-01-job'`).Scan(&node, &timeoutAt)
		return err == nil && node != "" && timeoutAt != ""
	})

	state, _ := jobRow(t, db, "reclaim-01-job")
	switch state {
	case "claimed", "processing", "completed":
		// claimed/processing: stamp observed live. completed: the claim,
		// stamp, and full lifecycle all happened inside the window - the
		// guarantee under test held end to end.
	default:
		t.Fatalf("job in unexpected state %q", state)
	}
	_ = nodeA
}

// TestClusterReclaim02_ExpiredRemoteClaimReclaimed: a job claimed by a
// remote node whose timeout_at has expired (crashed claimer) is reset to
// pending by THIS node's sweeper, then re-claimable. This is the shared-
// store sweep the daemon drives (StartReclaimSweeper -> ReclaimRemoteStale).
//
// The pending state is transient. Pool workers claim any pending job, so
// the row is usually re-stamped by node-a or node-b before a 200ms poll
// sees "pending". CI run 42329339354 timed out 20s waiting for that
// window under full-tier load; the ghost claim had already been released.
// The stable signal is that node-ghost no longer owns the row.
func TestClusterReclaim02_ExpiredRemoteClaimReclaimed(t *testing.T) {
	gossipA := "127.0.0.1:" + strconv.Itoa(harness.FreePort(t))
	gossipB := "127.0.0.1:" + strconv.Itoa(harness.FreePort(t))
	sharedQueue := filepath.Join(t.TempDir(), "shared-queue.db")

	_ = startNode(t, "node-a", sharedQueue, gossipA)
	_ = startNode(t, "node-b", sharedQueue, gossipB)

	db := openShared(t, sharedQueue)
	// Claimed by "node-ghost" (no such daemon - it can never heartbeat),
	// timeout already 2s in the past.
	seedClaimed(t, db, "reclaim-02-job", "node-ghost", -2*time.Second)

	deadline := time.Now().Add(sweepDeadline)
	var state, node string
	for {
		state, node = jobRow(t, db, "reclaim-02-job")
		if node != "node-ghost" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for expired remote claim to leave node-ghost; last state=%q node=%q",
				sweepDeadline, state, node)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Pending + empty stamp is the reset itself. A live node (or a
	// terminal state still carrying that node's stamp) means a pool
	// worker re-claimed the reset row before this poll — the
	// re-claimable outcome, not a missed reclaim.
	switch state {
	case "pending":
		if node != "" {
			t.Fatalf("pending row still stamped claimed_by_node=%q", node)
		}
	case "claimed", "processing", "completed", "failed":
		if node != "" && node != "node-a" && node != "node-b" {
			t.Fatalf("ghost claim released into unexpected owner state=%q node=%q", state, node)
		}
	default:
		t.Fatalf("ghost claim released into unexpected state=%q node=%q", state, node)
	}
}

// TestClusterReclaim03_UnexpiredRemoteClaimUntouched: a remote claim whose
// timeout_at is still in the future must survive the sweeper's tick - the
// false-positive guard. (The heartbeat side - extending timeout_at - is
// worker-path behavior covered by unit tests; this pins the sweeper's
// "not yet expired = hands off" half against real daemons.)
func TestClusterReclaim03_UnexpiredRemoteClaimUntouched(t *testing.T) {
	gossipA := "127.0.0.1:" + strconv.Itoa(harness.FreePort(t))
	gossipB := "127.0.0.1:" + strconv.Itoa(harness.FreePort(t))
	sharedQueue := filepath.Join(t.TempDir(), "shared-queue.db")

	_ = startNode(t, "node-a", sharedQueue, gossipA)
	_ = startNode(t, "node-b", sharedQueue, gossipB)

	db := openShared(t, sharedQueue)
	// Claimed by a remote node, timeout 60s in the future - far beyond
	// the observation window.
	seedClaimed(t, db, "reclaim-03-job", "node-remote", 60*time.Second)

	// Observe across several sweeper ticks.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		state, node := jobRow(t, db, "reclaim-03-job")
		if state == "pending" || (node != "node-remote" && state != "claimed") {
			t.Fatalf("unexpired remote claim was touched: state=%q node=%q", state, node)
		}
		time.Sleep(250 * time.Millisecond)
	}
	state, _ := jobRow(t, db, "reclaim-03-job")
	if state != "claimed" {
		t.Fatalf("final state %q, want claimed (untouched)", state)
	}
}
