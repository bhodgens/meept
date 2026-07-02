package integration

// dispatch_round_trip_test.go — End-to-end test of cross-daemon dispatch
// (spec §10). Two daemons (A, B). A dispatches a task to B with one
// required resource (a small file). Verify: B receives TASK_CREATE,
// materializes the resource (CAS miss on B → fetch via gRPC from A →
// store locally), runs (mock AgentInvoker reads the file), emits
// TASK_COMPLETE.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/pkg/id"
)

// TestDispatchRoundTrip verifies the full dispatch lifecycle: A → B
// with resource materialization and execution.
func TestDispatchRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create two daemons.
	daemonA := newTestDaemon(t, ctx, "node-a")
	daemonB := newTestDaemon(t, ctx, "node-b")
	defer daemonA.Close()
	defer daemonB.Close()

	// Connect them.
	connectPeers(daemonA, daemonB)
	waitForPeerConnection(t, daemonA, daemonB, 5*time.Second)
	waitForPeerConnection(t, daemonB, daemonA, 5*time.Second)

	// Add a file to A's CAS.
	fileContent := "hello from node A"
	fileHash := daemonA.addFileToCAS(t, ctx, fileContent)

	// Verify A has the blob.
	if !daemonA.resourceManager.Has(fileHash) {
		t.Fatal("daemon A should have the blob after Add")
	}

	// B should NOT have the blob yet.
	if daemonB.resourceManager.Has(fileHash) {
		t.Fatal("daemon B should NOT have the blob before fetch")
	}

	// B's executor bridge uses a mock invoker that reads the materialized
	// resource file. The ExecutorBridge calls ResourceManager.Ensure BEFORE
	// invoking the agent, so the file should be on disk by the time the
	// invoker runs. The invoker reads it via the CAS store.
	mockInvoker := &mockAgentInvoker{
		fn: func(ctx context.Context, job cluster.DispatchJob, worktreePath string) (string, []string, error) {
			// The resources have been materialized by ExecutorBridge.executeJob
			// before this runs. Read them from B's CAS.
			for _, rawRef := range job.RequiredResources {
				_, body, isCAS := resources.ParseRef(rawRef)
				if !isCAS {
					continue
				}
				path, err := daemonB.resourceManager.Store().GetPath(body)
				if err != nil {
					return "", nil, err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return "", nil, err
				}
				return string(data), nil, nil
			}
			return "no resources", nil, nil
		},
	}
	daemonB.executorBridge.SetAgentInvoker(mockInvoker)

	// A submits a job to B via gRPC DispatchService.Submit.
	pc, err := daemonA.grpcTransport.DialPeer(ctx, daemonB.nodeID, daemonB.listenAddr)
	if err != nil {
		t.Fatalf("failed to dial B: %v", err)
	}

	job := cluster.DispatchJob{
		JobID:             id.Generate("dispatch-test-"),
		OriginNode:        daemonA.nodeID,
		TargetNode:        daemonB.nodeID,
		AgentID:           "coder",
		TaskDescription:   "read the resource file",
		RequiredResources: []string{fileHash},
		CreatedAt:         time.Now().UnixNano(),
	}

	ack, err := pc.Submit(ctx, job)
	if err != nil {
		t.Fatalf("dispatch submit failed: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("dispatch was not accepted: %s", ack.Message)
	}

	// Wait for the job to reach a terminal state on B.
	// The executor bridge runs executeJob in a goroutine; it will
	// materialize resources (triggering CAS fetch from A), invoke the
	// agent, then complete.
	deadline := time.Now().Add(15 * time.Second)
	var status cluster.JobStatus
	found := false
	for time.Now().Before(deadline) {
		status, err = daemonB.executorBridge.JobStatus(ctx, job.JobID)
		if err != nil {
			t.Fatalf("JobStatus error: %v", err)
		}
		// "unknown" means the job has already been cleaned up from the
		// active map (completed or failed). Check the metrics.
		if status.State == "completed" {
			found = true
			break
		}
		if status.State == "unknown" {
			// Job may have completed too fast. Check metrics.
			if daemonB.metrics.DispatchJobsCompleted.Load() > 0 {
				found = true
				t.Log("job completed before poll caught it")
				break
			}
			if daemonB.metrics.DispatchJobsFailed.Load() > 0 {
				// Job failed (likely resource materialization error).
				t.Fatalf("job failed during execution (check peer fetch). dispatched_received=%d completed=%d failed=%d",
					daemonB.metrics.DispatchJobsReceived.Load(),
					daemonB.metrics.DispatchJobsCompleted.Load(),
					daemonB.metrics.DispatchJobsFailed.Load())
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !found {
		t.Fatalf("job never completed. state=%s received=%d completed=%d failed=%d",
			status.State,
			daemonB.metrics.DispatchJobsReceived.Load(),
			daemonB.metrics.DispatchJobsCompleted.Load(),
			daemonB.metrics.DispatchJobsFailed.Load())
	}

	// Verify B now has the blob in its CAS (fetched from A).
	if !daemonB.resourceManager.Has(fileHash) {
		t.Error("expected daemon B to have the blob after materialization")
	}

	// Verify the blob content matches.
	_, body, _ := resources.ParseRef(fileHash)
	path, err := daemonB.resourceManager.Store().GetPath(body)
	if err != nil {
		t.Fatalf("GetPath on B: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile on B: %v", err)
	}
	if string(data) != fileContent {
		t.Errorf("content mismatch: got %q, want %q", string(data), fileContent)
	}

	// Verify CAS miss/fetch counters.
	if daemonB.metrics.CASMisses.Load() == 0 {
		t.Error("expected CAS misses on B")
	}
	if daemonB.metrics.CASBytesFetched.Load() == 0 {
		t.Error("expected CAS bytes fetched on B")
	}
}

// TestDispatchCASFetchDirectly tests the CAS fetch path directly: B has a
// CAS miss and fetches from A via the peer fetcher.
func TestDispatchCASFetchDirectly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	daemonA := newTestDaemon(t, ctx, "fetch-source")
	daemonB := newTestDaemon(t, ctx, "fetch-sink")
	defer daemonA.Close()
	defer daemonB.Close()

	connectPeers(daemonA, daemonB)
	waitForPeerConnection(t, daemonB, daemonA, 5*time.Second)

	// Add a file to A.
	content := "CAS fetch test content"
	fileHash := daemonA.addFileToCAS(t, ctx, content)

	// B fetches via Ensure (triggers peer fetch).
	path, err := daemonB.resourceManager.Ensure(ctx, resources.ResourceRef{Raw: fileHash})
	if err != nil {
		t.Fatalf("Ensure on B failed: %v", err)
	}

	// Verify content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("content mismatch: got %q, want %q", string(data), content)
	}

	// Release the ref.
	daemonB.resourceManager.Release(resources.ResourceRef{Raw: fileHash})
}

// mockAgentInvoker implements cluster.AgentInvoker for tests.
type mockAgentInvoker struct {
	fn func(ctx context.Context, job cluster.DispatchJob, worktreePath string) (string, []string, error)
}

func (m *mockAgentInvoker) InvokeTask(ctx context.Context, job cluster.DispatchJob, worktreePath string) (string, []string, error) {
	if m.fn != nil {
		return m.fn(ctx, job, worktreePath)
	}
	return "mock output", nil, nil
}
