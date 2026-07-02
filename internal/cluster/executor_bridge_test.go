package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/pkg/models"
)

// --- Test fakes ---

// fakeAgentInvoker is a controllable AgentInvoker for tests.
type fakeAgentInvoker struct {
	mu           sync.Mutex
	invocations  []DispatchJob
	output       string
	outputRes    []string
	err          error
	delay        time.Duration
	panicValue   any
	callCount    int32
}

func (f *fakeAgentInvoker) InvokeTask(ctx context.Context, job DispatchJob, worktreePath string) (string, []string, error) {
	atomic.AddInt32(&f.callCount, 1)
	f.mu.Lock()
	f.invocations = append(f.invocations, job)
	out := f.output
	res := f.outputRes
	err := f.err
	delay := f.delay
	panicVal := f.panicValue
	f.mu.Unlock()

	if panicVal != nil {
		panic(panicVal)
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	return out, res, err
}

func (f *fakeAgentInvoker) calls() int {
	return int(atomic.LoadInt32(&f.callCount))
}

// fakeBusPublisher captures published events.
type fakeBusPublisher struct {
	mu        sync.Mutex
	completes []completeEvent
	fails     []failEvent
}

type completeEvent struct {
	JobID     string
	OutputRef string
	Workspace *WorkspaceRef
}

type failEvent struct {
	JobID  string
	Reason string
}

func (p *fakeBusPublisher) PublishTaskComplete(jobID, outputRef string, ws *WorkspaceRef) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completes = append(p.completes, completeEvent{JobID: jobID, OutputRef: outputRef, Workspace: ws})
}

func (p *fakeBusPublisher) PublishTaskFail(jobID, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fails = append(p.fails, failEvent{JobID: jobID, Reason: reason})
}

func (p *fakeBusPublisher) snapshot() ([]completeEvent, []failEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := make([]completeEvent, len(p.completes))
	copy(c, p.completes)
	f := make([]failEvent, len(p.fails))
	copy(f, p.fails)
	return c, f
}

// fakeResourceManager is a minimal ResourceManager for refcount testing.
type fakeResourceManager struct {
	mu          sync.Mutex
	ensured     map[string]int // raw ref → refcount
	released    map[string]int // raw ref → release count
	ensureErr   error          // when set, Ensure returns this
	addCount    int
}

func newFakeResourceManager() *fakeResourceManager {
	return &fakeResourceManager{
		ensured:  make(map[string]int),
		released: make(map[string]int),
	}
}

func (m *fakeResourceManager) Ensure(ctx context.Context, ref resources.ResourceRef) (string, error) {
	m.mu.Lock()
	ensureErr := m.ensureErr
	m.mu.Unlock()
	if ensureErr != nil {
		return "", ensureErr
	}

	m.mu.Lock()
	m.ensured[ref.Raw]++
	path := "/fake/" + ref.Raw
	m.mu.Unlock()
	return path, nil
}

func (m *fakeResourceManager) Release(ref resources.ResourceRef) {
	m.mu.Lock()
	m.released[ref.Raw]++
	m.mu.Unlock()
}

func (m *fakeResourceManager) Add(ctx context.Context, srcPath string) (string, error) {
	m.mu.Lock()
	m.addCount++
	m.mu.Unlock()
	return "blake3:fakehash" + srcPath, nil
}

func (m *fakeResourceManager) Has(hash string) bool { return true }

func (m *fakeResourceManager) ensuredCount(raw string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensured[raw]
}

func (m *fakeResourceManager) releasedCount(raw string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.released[raw]
}

// --- Tests ---

// Helper: build a TASK_CREATE event with the wrapped dispatch_job form.
func taskCreateEvent(job DispatchJob) *models.ClusterEvent {
	payload, _ := encodeDispatchJob(job)
	return &models.ClusterEvent{
		EventID:   "evt-test-1",
		NodeID:    "node-origin",
		EventType: models.EventTaskCreate,
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

// Helper: wait for a job to reach a terminal state. Returns true if
// completed within timeout.
func waitForJob(aj *activeJob, timeout time.Duration) bool {
	select {
	case <-aj.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func TestExecutorBridge_HandleTaskCreate_Lifecycle(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "result", outputRes: []string{"blake3:out1"}}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{
		JobID:           "job-1",
		OriginNode:      "node-origin",
		AgentID:         "agent-a",
		TaskDescription: "do thing",
	}

	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	// Wait for the goroutine to complete.
	bridge.mu.Lock()
	aj := bridge.active["job-1"]
	bridge.mu.Unlock()
	if aj == nil {
		// Already finished — check publisher.
	} else {
		if !waitForJob(aj, 2*time.Second) {
			t.Fatalf("job did not complete within timeout")
		}
	}

	completes, fails := pub.snapshot()
	if len(fails) > 0 {
		t.Fatalf("unexpected failures: %+v", fails)
	}
	if len(completes) != 1 {
		t.Fatalf("want 1 completion, got %d", len(completes))
	}
	if completes[0].JobID != "job-1" {
		t.Fatalf("completion job ID: want job-1, got %s", completes[0].JobID)
	}
	if completes[0].OutputRef != "blake3:out1" {
		t.Fatalf("output ref: want blake3:out1, got %s", completes[0].OutputRef)
	}

	if invoker.calls() != 1 {
		t.Fatalf("invoker call count: want 1, got %d", invoker.calls())
	}
}

func TestExecutorBridge_HandleTaskCreate_BareTaskPayload(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "ok"}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	// Bare TaskPayload form.
	tp := models.TaskPayload{
		TaskID:      "task-123",
		AgentID:     "agent-x",
		Description: "do thing",
		Priority:    5,
	}
	payload, _ := json.Marshal(tp)
	event := &models.ClusterEvent{
		EventID:   "evt-2",
		NodeID:    "node-origin",
		EventType: models.EventTaskCreate,
		Timestamp: time.Now(),
		Payload:   payload,
	}

	if err := bridge.HandleTaskCreate(event); err != nil {
		t.Fatalf("HandleTaskCreate bare payload: %v", err)
	}

	// The synthesized job ID is "dispatch-task-123".
	bridge.mu.Lock()
	aj := bridge.active["dispatch-task-123"]
	bridge.mu.Unlock()
	if aj != nil {
		if !waitForJob(aj, 2*time.Second) {
			t.Fatalf("bare-payload job did not complete within timeout")
		}
	}

	completes, fails := pub.snapshot()
	if len(fails) > 0 {
		t.Fatalf("unexpected failures: %+v", fails)
	}
	if len(completes) != 1 {
		t.Fatalf("want 1 completion, got %d", len(completes))
	}
	if completes[0].JobID != "dispatch-task-123" {
		t.Fatalf("synthesized job ID: want dispatch-task-123, got %s", completes[0].JobID)
	}

	// Check invoker received the synthesized job.
	invoker.mu.Lock()
	if len(invoker.invocations) != 1 {
		invoker.mu.Unlock()
		t.Fatalf("want 1 invoker call, got %d", len(invoker.invocations))
	}
	if invoker.invocations[0].AgentID != "agent-x" {
		t.Fatalf("agent_id: want agent-x, got %s", invoker.invocations[0].AgentID)
	}
	invoker.mu.Unlock()
}

func TestExecutorBridge_HandleTaskCreate_AgentError(t *testing.T) {
	invoker := &fakeAgentInvoker{err: errors.New("agent exploded")}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{JobID: "job-err", OriginNode: "origin"}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-err"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	completes, fails := pub.snapshot()
	if len(completes) != 0 {
		t.Fatalf("want 0 completions on agent error, got %d", len(completes))
	}
	if len(fails) != 1 {
		t.Fatalf("want 1 failure, got %d", len(fails))
	}
	if !strings.Contains(fails[0].Reason, "agent exploded") {
		t.Fatalf("fail reason should contain agent error, got %q", fails[0].Reason)
	}
}

func TestExecutorBridge_HandleTaskCreate_AgentPanic(t *testing.T) {
	invoker := &fakeAgentInvoker{panicValue: "boom"}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{JobID: "job-panic", OriginNode: "origin"}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-panic"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	completes, fails := pub.snapshot()
	if len(completes) != 0 {
		t.Fatalf("want 0 completions on panic, got %d", len(completes))
	}
	if len(fails) != 1 {
		t.Fatalf("want 1 failure on panic, got %d", len(fails))
	}
	if !strings.Contains(fails[0].Reason, "panic") {
		t.Fatalf("fail reason should mention panic, got %q", fails[0].Reason)
	}
	if !strings.Contains(fails[0].Reason, "boom") {
		t.Fatalf("fail reason should contain panic value, got %q", fails[0].Reason)
	}
}

func TestExecutorBridge_NoInvoker_FailsJob(t *testing.T) {
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	// NOTE: no SetAgentInvoker.
	bridge.SetBusPublisher(pub)

	job := DispatchJob{JobID: "job-noinvoker", OriginNode: "origin"}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-noinvoker"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	_, fails := pub.snapshot()
	if len(fails) != 1 {
		t.Fatalf("want 1 failure, got %d", len(fails))
	}
	if !strings.Contains(fails[0].Reason, "not configured") {
		t.Fatalf("reason should mention not configured, got %q", fails[0].Reason)
	}
}

func TestExecutorBridge_ResourceEnsureFailure(t *testing.T) {
	rm := newFakeResourceManager()
	rm.ensureErr = resources.ErrResourceUnavailable

	invoker := &fakeAgentInvoker{output: "should-not-reach"}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetResourceManager(rm)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{
		JobID:             "job-rsrc-fail",
		OriginNode:        "origin",
		RequiredResources: []string{"blake3:abc", "blake3:def"},
	}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-rsrc-fail"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	_, fails := pub.snapshot()
	if len(fails) != 1 {
		t.Fatalf("want 1 failure, got %d", len(fails))
	}
	if !strings.Contains(fails[0].Reason, "materialize resources") {
		t.Fatalf("reason should mention materialize resources, got %q", fails[0].Reason)
	}

	if invoker.calls() != 0 {
		t.Fatalf("invoker should not be called on resource failure, got %d calls", invoker.calls())
	}
}

func TestExecutorBridge_RefcountDecrementOnComplete(t *testing.T) {
	rm := newFakeResourceManager()
	invoker := &fakeAgentInvoker{output: "ok"}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetResourceManager(rm)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{
		JobID:             "job-refs",
		OriginNode:        "origin",
		RequiredResources: []string{"blake3:abc", "blake3:def"},
	}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-refs"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	// Each resource should have been Ensured then Released.
	for _, raw := range []string{"blake3:abc", "blake3:def"} {
		if got := rm.ensuredCount(raw); got != 1 {
			t.Fatalf("Ensure count for %s: want 1, got %d", raw, got)
		}
		if got := rm.releasedCount(raw); got != 1 {
			t.Fatalf("Release count for %s: want 1, got %d", raw, got)
		}
	}
}

func TestExecutorBridge_RefcountDecrementOnFailure(t *testing.T) {
	rm := newFakeResourceManager()
	invoker := &fakeAgentInvoker{err: errors.New("agent fail")}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetResourceManager(rm)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{
		JobID:             "job-refs-fail",
		OriginNode:        "origin",
		RequiredResources: []string{"blake3:abc"},
	}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-refs-fail"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	if got := rm.releasedCount("blake3:abc"); got != 1 {
		t.Fatalf("Release count on failure: want 1, got %d", got)
	}
}

func TestExecutorBridge_RefcountReleaseOnPartialEnsureFailure(t *testing.T) {
	// Two resources; first succeeds, second fails. The successful one
	// must be Released to avoid a refcount leak.
	rm := &selectiveResourceManager{
		failOnRef: "blake3:bad",
		ensured:   make(map[string]int),
		released:  make(map[string]int),
	}

	invoker := &fakeAgentInvoker{output: "ok"}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetResourceManager(rm)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{
		JobID:             "job-partial",
		OriginNode:        "origin",
		RequiredResources: []string{"blake3:good", "blake3:bad"},
	}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-partial"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	// The good resource was Ensured and then Released during the
	// partial-failure cleanup path.
	if got := rm.ensured["blake3:good"]; got != 1 {
		t.Fatalf("Ensure count for good: want 1, got %d", got)
	}
	if got := rm.released["blake3:good"]; got != 1 {
		t.Fatalf("Release count for good on partial failure: want 1, got %d", got)
	}
}

// selectiveResourceManager succeeds on all refs except failOnRef.
type selectiveResourceManager struct {
	ensured   map[string]int
	released  map[string]int
	failOnRef string
	mu        sync.Mutex
}

func (m *selectiveResourceManager) Ensure(ctx context.Context, ref resources.ResourceRef) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ref.Raw == m.failOnRef {
		return "", resources.ErrResourceUnavailable
	}
	m.ensured[ref.Raw]++
	return "/fake/" + ref.Raw, nil
}

func (m *selectiveResourceManager) Release(ref resources.ResourceRef) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released[ref.Raw]++
}

func (m *selectiveResourceManager) Add(ctx context.Context, srcPath string) (string, error) {
	return "blake3:added", nil
}

func (m *selectiveResourceManager) Has(hash string) bool { return true }

func TestExecutorBridge_DuplicateJobIgnored(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "ok", delay: 500 * time.Millisecond}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{JobID: "job-dup", OriginNode: "origin"}

	// First submit.
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate first: %v", err)
	}
	// Second submit (duplicate) — should be accepted (returns nil) but
	// not start a second goroutine.
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate duplicate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-dup"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 3*time.Second)
	}

	// Only one invoker call should have happened.
	if invoker.calls() != 1 {
		t.Fatalf("duplicate job: want 1 invoker call, got %d", invoker.calls())
	}
}

func TestExecutorBridge_SettersNilGuard(t *testing.T) {
	bridge := NewExecutorBridge("local", nil)
	// All setters should accept nil without panic.
	bridge.SetMetrics(nil)
	bridge.SetResourceManager(nil)
	bridge.SetWorkspaceManager(nil)
	bridge.SetAgentInvoker(nil)
	bridge.SetBusPublisher(nil)
}

func TestExecutorBridge_MetricsIncremented(t *testing.T) {
	metrics := NewMetrics()
	invoker := &fakeAgentInvoker{output: "ok"}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetMetrics(metrics)
	bridge.SetAgentInvoker(invoker)

	job := DispatchJob{JobID: "job-metrics", OriginNode: "origin"}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-metrics"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	snap := metrics.Snapshot()
	if snap.DispatchJobsReceived != 1 {
		t.Fatalf("dispatch_jobs_received: want 1, got %d", snap.DispatchJobsReceived)
	}
	if snap.DispatchJobsCompleted != 1 {
		t.Fatalf("dispatch_jobs_completed: want 1, got %d", snap.DispatchJobsCompleted)
	}
	if snap.DispatchJobsFailed != 0 {
		t.Fatalf("dispatch_jobs_failed: want 0, got %d", snap.DispatchJobsFailed)
	}
}

func TestExecutorBridge_MetricsIncrementedOnFailure(t *testing.T) {
	metrics := NewMetrics()
	invoker := &fakeAgentInvoker{err: errors.New("fail")}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetMetrics(metrics)
	bridge.SetAgentInvoker(invoker)

	job := DispatchJob{JobID: "job-metrics-fail", OriginNode: "origin"}
	if err := bridge.HandleTaskCreate(taskCreateEvent(job)); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	bridge.mu.Lock()
	aj := bridge.active["job-metrics-fail"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	snap := metrics.Snapshot()
	if snap.DispatchJobsFailed != 1 {
		t.Fatalf("dispatch_jobs_failed: want 1, got %d", snap.DispatchJobsFailed)
	}
}

func TestExecutorBridge_SubmitJob(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "ok"}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)

	job := DispatchJob{JobID: "submit-1", OriginNode: "origin"}

	ctx := context.Background()
	ack, err := bridge.SubmitJob(ctx, job)
	if err != nil {
		t.Fatalf("SubmitJob error: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("ack should be accepted, got %+v", ack)
	}
	if ack.JobID != "submit-1" {
		t.Fatalf("ack job ID: want submit-1, got %s", ack.JobID)
	}

	bridge.mu.Lock()
	aj := bridge.active["submit-1"]
	bridge.mu.Unlock()
	if aj != nil {
		waitForJob(aj, 2*time.Second)
	}

	if invoker.calls() != 1 {
		t.Fatalf("invoker should be called once, got %d", invoker.calls())
	}
}

func TestExecutorBridge_JobStatus(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "ok", delay: 200 * time.Millisecond}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)

	job := DispatchJob{JobID: "status-1", OriginNode: "origin"}
	_ = bridge.HandleTaskCreate(taskCreateEvent(job))

	ctx := context.Background()

	// While running.
	status, err := bridge.JobStatus(ctx, "status-1")
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if status.JobID != "status-1" {
		t.Fatalf("status JobID: want status-1, got %s", status.JobID)
	}
	if status.State != "running" && status.State != "completed" {
		t.Fatalf("status: want running or completed, got %s", status.State)
	}

	// Unknown.
	status, _ = bridge.JobStatus(ctx, "unknown-job")
	if status.State != "unknown" {
		t.Fatalf("unknown job: want unknown, got %s", status.State)
	}
}

func TestExecutorBridge_ContextCancelCleansUp(t *testing.T) {
	rm := newFakeResourceManager()
	invoker := &fakeAgentInvoker{delay: 30 * time.Second} // very long
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetResourceManager(rm)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)

	job := DispatchJob{
		JobID:             "job-cancel",
		OriginNode:        "origin",
		RequiredResources: []string{"blake3:abc"},
	}
	_ = bridge.HandleTaskCreate(taskCreateEvent(job))

	// Give the goroutine a moment to ensure resources.
	time.Sleep(100 * time.Millisecond)

	// Find and cancel the job's context.
	bridge.mu.Lock()
	aj := bridge.active["job-cancel"]
	bridge.mu.Unlock()
	if aj == nil {
		t.Fatalf("job not found in active map")
	}
	aj.cancel()

	waitForJob(aj, 2*time.Second)

	// Resource should be Released on cancel.
	if got := rm.releasedCount("blake3:abc"); got != 1 {
		t.Fatalf("release on cancel: want 1, got %d", got)
	}

	_, fails := pub.snapshot()
	if len(fails) != 1 {
		t.Fatalf("cancel should produce 1 failure, got %d", len(fails))
	}
}

func TestExecutorBridge_NilPayloadReturnsError(t *testing.T) {
	bridge := NewExecutorBridge("local", nil)
	event := &models.ClusterEvent{
		EventID:   "evt-empty",
		EventType: models.EventTaskCreate,
	}
	err := bridge.HandleTaskCreate(event)
	if err == nil {
		t.Fatalf("empty payload should return error")
	}
}

func TestExecutorBridge_NilEventReturnsNil(t *testing.T) {
	bridge := NewExecutorBridge("local", nil)
	if err := bridge.HandleTaskCreate(nil); err != nil {
		t.Fatalf("nil event should return nil, got %v", err)
	}
}

func TestExecutorBridge_AutoGenerateJobID(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "ok"}
	bridge := NewExecutorBridge("local", nil)
	bridge.SetAgentInvoker(invoker)

	job := DispatchJob{
		// JobID intentionally empty.
		OriginNode: "origin",
		AgentID:    "a",
	}
	_ = bridge.HandleTaskCreate(taskCreateEvent(job))

	// Wait for the async goroutine to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if invoker.calls() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The bridge should have generated a job ID and invoked the agent.
	if invoker.calls() != 1 {
		t.Fatalf("auto-generated ID: invoker should be called once, got %d", invoker.calls())
	}
}

func TestDecodeDispatchJob_WrappedForm(t *testing.T) {
	job := DispatchJob{
		JobID:      "wrapped-1",
		OriginNode: "node-origin",
		AgentID:    "agent-a",
		Priority:   3,
	}
	payload, _ := encodeDispatchJob(job)

	decoded, err := decodeDispatchJob(payload, "node-origin")
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.JobID != "wrapped-1" {
		t.Fatalf("job ID: want wrapped-1, got %s", decoded.JobID)
	}
	if decoded.AgentID != "agent-a" {
		t.Fatalf("agent ID: want agent-a, got %s", decoded.AgentID)
	}
	if decoded.OriginNode != "node-origin" {
		t.Fatalf("origin: want node-origin, got %s", decoded.OriginNode)
	}
}

func TestDecodeDispatchJob_WrappedForm_FillsOriginFromEvent(t *testing.T) {
	job := DispatchJob{
		JobID:   "wrapped-2",
		AgentID: "agent-b",
		// OriginNode intentionally empty.
	}
	payload, _ := encodeDispatchJob(job)

	decoded, err := decodeDispatchJob(payload, "node-event")
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.OriginNode != "node-event" {
		t.Fatalf("origin should be filled from event source, want node-event, got %s", decoded.OriginNode)
	}
}

func TestDecodeDispatchJob_BareForm(t *testing.T) {
	tp := models.TaskPayload{
		TaskID:      "task-99",
		AgentID:     "agent-z",
		Description: "test",
		Priority:    2,
	}
	payload, _ := json.Marshal(tp)

	decoded, err := decodeDispatchJob(payload, "node-origin")
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.JobID != "dispatch-task-99" {
		t.Fatalf("job ID: want dispatch-task-99, got %s", decoded.JobID)
	}
	if decoded.AgentID != "agent-z" {
		t.Fatalf("agent ID: want agent-z, got %s", decoded.AgentID)
	}
	if decoded.TaskDescription != "test" {
		t.Fatalf("description: want test, got %s", decoded.TaskDescription)
	}
}

func TestDecodeDispatchJob_Garbage(t *testing.T) {
	_, err := decodeDispatchJob(json.RawMessage("not json at all"), "node-origin")
	if err == nil {
		t.Fatalf("garbage payload should return error")
	}
}

// --- helpers ---

