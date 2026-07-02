package cluster

// executor_bridge.go — the glue between cluster dispatch and the local
// agent execution layer.
//
// ExecutorBridge subscribes (via HandleTaskCreate) to TASK_CREATE events
// from the gossip handler. It materializes required resources (parallel
// Ensure calls), materializes the workspace, invokes the agent loop via
// the AgentInvoker interface, then emits TASK_COMPLETE / TASK_FAIL back
// into the bus.
//
// Module boundary (spec §3.3): this is the only piece that knows about
// both the cluster and agent layers. To avoid an import cycle on
// internal/agent (which imports internal/cluster transitively), the
// agent invoker is abstracted behind a small interface that the daemon
// injects via SetAgentInvoker.
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §4.4, §3.2, §5 Phase 3-5, §6, §8

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/internal/workspace"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// AgentInvoker abstracts the agent-loop invocation so the cluster package
// does not need to import internal/agent. The daemon constructs a small
// adapter that wraps *agent.AgentLoop and injects it via SetAgentInvoker.
//
// If InvokeTask panics, executeJob's deferred recover captures it and
// routes the job through failJob with the panic message (spec §6
// executor-bridge: "Agent loop panics → Recover, log stack, emit
// TASK_FAIL with panic info").
type AgentInvoker interface {
	// InvokeTask runs the agent loop for a dispatched job. The
	// worktreePath is the materialized workspace root; the agent's cwd
	// MUST be set to it (or empty when no workspace was requested).
	// Returns the agent's textual output plus any output resource hashes
	// (prefixed, e.g. "blake3:...") that should be advertised in the
	// DispatchResult.
	InvokeTask(ctx context.Context, job DispatchJob, worktreePath string) (output string, outputResources []string, err error)
}

// BusPublisher abstracts the event-publish surface so ExecutorBridge
// does not depend on *bus.MessageBus directly (matches the employee
// package pattern referenced in MEMORY.md). When nil, completion and
// failure events are logged but not published.
type BusPublisher interface {
	// PublishTaskComplete emits a TASK_COMPLETE event carrying the
	// DispatchResult envelope. workspace may be nil.
	PublishTaskComplete(jobID, outputRef string, workspace *WorkspaceRef)
	// PublishTaskFail emits a TASK_FAIL event with a human-readable
	// reason.
	PublishTaskFail(jobID, reason string)
}

// activeJob tracks one in-flight dispatch job. Kept in
// ExecutorBridge.active under the bridge's mutex.
type activeJob struct {
	job          DispatchJob
	cancel       context.CancelFunc
	worktreePath string         // empty when no workspace was materialized
	resourceRefs []resources.ResourceRef // for Release on cleanup
	startedAt    time.Time
	// done is closed when executeJob returns (whether via completeJob,
	// failJob, or context cancel). Tests can wait on it.
	done chan struct{}
	// finished is set true by completeJob/failJob under the bridge mutex
	// to make the "first closer wins" idempotent.
	finished bool
}

// ExecutorBridge is the runtime component that accepts dispatched tasks
// from the cluster mesh, materializes their resource/workspace
// requirements, runs the local agent loop, and publishes results back.
//
// All setters are nil-guarded. Construct with NewExecutorBridge.
type ExecutorBridge struct {
	localNodeID string
	logger      *slog.Logger
	metrics     *Metrics // nil-safe (every Inc helper nil-guards)

	resources  resources.ResourceManager // nil-safe (Ensure is skipped)
	workspaces workspace.WorkspaceManager // nil-safe (Ensure is skipped)
	invoker    AgentInvoker               // nil-safe (returns error)
	publisher  BusPublisher               // nil-safe (logs only)

	mu     sync.Mutex
	active map[string]*activeJob // jobID -> active job state
}

// NewExecutorBridge constructs a bridge with the required localNodeID
// and logger. Resource/workspace/invoker/publisher dependencies are
// injected via their Set* methods (all nil-guarded).
//
// We do not accept the ResourceManager/WorkspaceManager in the
// constructor because the daemon wiring order varies — the bridge may be
// constructed before those components exist, then wired in a second
// pass.
func NewExecutorBridge(localNodeID string, logger *slog.Logger) *ExecutorBridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExecutorBridge{
		localNodeID: localNodeID,
		logger:      logger.With("component", "executor_bridge"),
		active:      make(map[string]*activeJob),
	}
}

// SetMetrics attaches the cluster Metrics struct. Nil-guarded.
func (b *ExecutorBridge) SetMetrics(m *Metrics) {
	if m != nil {
		b.metrics = m
	}
}

// SetResourceManager attaches the CAS resource manager. Nil-guarded.
func (b *ExecutorBridge) SetResourceManager(r resources.ResourceManager) {
	if r != nil {
		b.resources = r
	}
}

// SetWorkspaceManager attaches the workspace manager. Nil-guarded.
func (b *ExecutorBridge) SetWorkspaceManager(w workspace.WorkspaceManager) {
	if w != nil {
		b.workspaces = w
	}
}

// SetAgentInvoker attaches the agent-loop invoker. Nil-guarded. When
// nil during executeJob, the job fails with "agent invoker not
// configured".
func (b *ExecutorBridge) SetAgentInvoker(a AgentInvoker) {
	if a != nil {
		b.invoker = a
	}
}

// SetBusPublisher attaches the event publisher. Nil-guarded. When nil,
// completion/failure are logged but not published to the cluster.
func (b *ExecutorBridge) SetBusPublisher(p BusPublisher) {
	if p != nil {
		b.publisher = p
	}
}

// HandleTaskCreate is the entry point from the gossip handler. It is
// called when a TASK_CREATE event arrives (spec §5 Phase 2 Receive).
//
// It accepts EITHER:
//   - a bare TaskPayload (legacy form; RequiredResources=[],
//     Workspace=nil), OR
//   - a wrapped payload with a "dispatch_job" field carrying the full
//     DispatchJob.
//
// In both cases it builds a DispatchJob, records it in the active map,
// and starts executeJob in a goroutine. Returns nil quickly so the
// gossip handler is not blocked.
//
// The event is always accepted (returns nil) unless the payload cannot
// be decoded at all — in which case the error propagates to the gossip
// engine for logging.
func (b *ExecutorBridge) HandleTaskCreate(event *models.ClusterEvent) error {
	if event == nil {
		return nil
	}
	if len(event.Payload) == 0 {
		return fmt.Errorf("executor bridge: empty payload on TASK_CREATE event %s", event.EventID)
	}

	job, err := decodeDispatchJob(event.Payload, event.NodeID)
	if err != nil {
		return fmt.Errorf("executor bridge: decode TASK_CREATE payload: %w", err)
	}

	if job.JobID == "" {
		job.JobID = id.Generate("dispatch-")
	}
	if job.CreatedAt == 0 {
		job.CreatedAt = time.Now().UnixNano()
	}
	if job.OriginNode == "" {
		job.OriginNode = event.NodeID
	}

	// TODO(spec §5 Phase 2): validate ed25519 signature when
	// job.Signature is present. The verification key comes from the
	// cluster registry (nodeID → pubkey). For now we accept unsigned
	// jobs — the gossip engine already verifies event-level signatures.

	ctx, cancel := context.WithCancel(context.Background())
	aj := &activeJob{
		job:       job,
		cancel:    cancel,
		startedAt: time.Now(),
		done:      make(chan struct{}),
	}

	b.mu.Lock()
	if _, exists := b.active[job.JobID]; exists {
		b.mu.Unlock()
		cancel()
		// Duplicate dispatch — already running. Log and accept.
		b.logger.Warn("executor bridge: duplicate TASK_CREATE for in-flight job",
			"job_id", job.JobID,
			"origin_node", job.OriginNode,
		)
		return nil
	}
	b.active[job.JobID] = aj
	b.mu.Unlock()

	b.metrics.IncDispatchJobsReceived()

	b.logger.Info("executor bridge: accepted dispatched job",
		"job_id", job.JobID,
		"origin_node", job.OriginNode,
		"agent_id", job.AgentID,
		"resources", len(job.RequiredResources),
		"has_workspace", job.Workspace != nil,
	)

	go b.executeJob(ctx, aj)

	return nil
}

// executeJob is the goroutine-side implementation (spec §5 Phase 3
// Materialize, Phase 4 Execute, Phase 5 Complete). It is called in a
// goroutine by HandleTaskCreate.
//
// Steps:
//  1. Materialize resources in parallel (each via ResourceManager.Ensure).
//  2. Materialize workspace via WorkspaceManager.Ensure (if job.Workspace).
//  3. Invoke the agent via AgentInvoker (with panic recovery).
//  4. completeJob on success, failJob on any failure (including panic
//     and context cancel).
//
// Idempotent terminal transition: completeJob/failJob use the
// activeJob.finished flag under the bridge mutex so the first closer
// wins even if a cancel races with a normal completion.
func (b *ExecutorBridge) executeJob(ctx context.Context, aj *activeJob) {
	defer close(aj.done)
	defer func() {
		if r := recover(); r != nil {
			b.failJob(aj, fmt.Errorf("agent invoker panic: %v", r))
		}
	}()

	job := aj.job

	// 1. Materialize resources in parallel.
	refs, err := b.materializeResources(ctx, job)
	if err != nil {
		b.failJob(aj, fmt.Errorf("materialize resources: %w", err))
		return
	}
	aj.resourceRefs = refs

	// 2. Materialize workspace.
	if job.Workspace != nil {
		wtPath, err := b.materializeWorkspace(ctx, *job.Workspace)
		if err != nil {
			b.failJob(aj, fmt.Errorf("materialize workspace: %w", err))
			return
		}
		aj.worktreePath = wtPath
	}

	// 3. Invoke agent.
	if b.invoker == nil {
		b.logger.Warn("executor bridge: agent invoker not configured, failing job",
			"job_id", job.JobID,
		)
		b.failJob(aj, errors.New("agent invoker not configured"))
		return
	}

	output, outputResources, err := b.invoker.InvokeTask(ctx, job, aj.worktreePath)
	if err != nil {
		// Context cancel is a clean teardown, not a failure-with-stack.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			b.logger.Info("executor bridge: job cancelled",
				"job_id", job.JobID,
				"err", err,
			)
			b.failJob(aj, err)
			return
		}
		b.failJob(aj, fmt.Errorf("agent invoker: %w", err))
		return
	}

	// 4. Complete. Build an output ref from the first output resource
	// hash (or the textual output when no resources were produced).
	outputRef := ""
	if len(outputResources) > 0 {
		outputRef = outputResources[0]
	} else if output != "" {
		// Hash the textual output via ResourceManager.Add when available
		// so the dispatcher can fetch it from CAS. When no ResourceManager
		// is wired, fall back to the raw text.
		if b.resources != nil {
			tmpPath, tmpErr := writeTempOutput(output)
			if tmpErr == nil {
				hash, addErr := b.resources.Add(ctx, tmpPath)
				// Clean up the temp file regardless of Add outcome.
				_ = os.Remove(tmpPath)
				if addErr == nil {
					outputRef = hash
				} else {
					b.logger.Warn("executor bridge: failed to add output to CAS",
						"job_id", job.JobID, "err", addErr)
				}
			} else {
				b.logger.Warn("executor bridge: failed to write temp output",
					"job_id", job.JobID, "err", tmpErr)
			}
		}
	}

	b.completeJob(aj, outputRef)
}

// materializeResources resolves each RequiredResources entry in
// parallel. Returns the resolved ResourceRefs (for later Release) or
// the first error encountered. On error, any refs that were already
// Ensured are Released to avoid refcount leaks.
//
// Refs use the resources.ResourceRef{Raw: entry} form — entries are
// expected to be prefixed hashes like "blake3:abcd...".
func (b *ExecutorBridge) materializeResources(ctx context.Context, job DispatchJob) ([]resources.ResourceRef, error) {
	if b.resources == nil {
		if len(job.RequiredResources) == 0 {
			return nil, nil
		}
		return nil, errors.New("resource manager not configured but job requires resources")
	}
	if len(job.RequiredResources) == 0 {
		return nil, nil
	}

	type result struct {
		index int
		ref   resources.ResourceRef
		path  string
		err   error
	}

	jobs := make([]string, len(job.RequiredResources))
	copy(jobs, job.RequiredResources)

	resultsCh := make(chan result, len(jobs))
	var wg sync.WaitGroup

	for i, raw := range jobs {
		wg.Add(1)
		go func(idx int, rawRef string) {
			defer wg.Done()
			ref := resources.ResourceRef{Raw: rawRef}
			path, err := b.resources.Ensure(ctx, ref)
			if err != nil {
				resultsCh <- result{index: idx, ref: ref, err: err}
				return
			}
			resultsCh <- result{index: idx, ref: ref, path: path}
		}(i, raw)
	}

	wg.Wait()
	close(resultsCh)

	// Collect results, capturing errors. We capture ALL results so we
	// can Release the successful ones on partial failure.
	all := make([]result, 0, len(jobs))
	for r := range resultsCh {
		all = append(all, r)
	}

	// Find first error.
	var firstErr error
	for _, r := range all {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			// Emit peer_unreachable metric on ErrResourceUnavailable.
			if errors.Is(r.err, resources.ErrResourceUnavailable) {
				b.metrics.IncPeerUnreachable()
			}
		}
	}

	if firstErr != nil {
		// Release any successfully Ensured refs to avoid refcount leaks.
		for _, r := range all {
			if r.err == nil {
				b.resources.Release(r.ref)
			}
		}
		return nil, firstErr
	}

	// Order by index for determinism.
	refs := make([]resources.ResourceRef, len(jobs))
	for _, r := range all {
		refs[r.index] = r.ref
	}
	return refs, nil
}

// materializeWorkspace wraps WorkspaceManager.Ensure with metrics and
// error translation.
func (b *ExecutorBridge) materializeWorkspace(ctx context.Context, ref WorkspaceRef) (string, error) {
	if b.workspaces == nil {
		return "", errors.New("workspace manager not configured but job has a workspace ref")
	}

	// Translate cluster-local WorkspaceRef to the workspace package's
	// type. They share a structural shape; we copy fields explicitly to
	// avoid coupling on type aliasing.
	wsRef := workspace.WorkspaceRef{
		RepoURL:      ref.RepoURL,
		CommitSHA:    ref.CommitSHA,
		DiffBlobHash: ref.DiffBlobHash,
		Dirty:        ref.Dirty,
	}

	path, err := b.workspaces.Ensure(ctx, wsRef)
	if err != nil {
		return "", err
	}
	return path, nil
}

// completeJob is the success path (spec §5 Phase 5). It captures the
// output, builds a DispatchResult, emits TASK_COMPLETE, and decrements
// refcounts on this job's resources. Idempotent via aj.finished.
func (b *ExecutorBridge) completeJob(aj *activeJob, outputRef string) {
	b.mu.Lock()
	if aj.finished {
		b.mu.Unlock()
		return
	}
	aj.finished = true
	b.mu.Unlock()

	b.cleanup(aj)

	b.metrics.IncDispatchJobsCompleted()

	wsRef := b.snapshotWorkspace(aj)

	if b.publisher != nil {
		b.publisher.PublishTaskComplete(aj.job.JobID, outputRef, wsRef)
	}

	b.logger.Info("executor bridge: job completed",
		"job_id", aj.job.JobID,
		"output_ref", outputRef,
	)
}

// failJob is the failure path (spec §6 executor-bridge). It emits
// TASK_FAIL, decrements refcounts, cleans up. Idempotent via
// aj.finished.
func (b *ExecutorBridge) failJob(aj *activeJob, err error) {
	b.mu.Lock()
	if aj.finished {
		b.mu.Unlock()
		return
	}
	aj.finished = true
	b.mu.Unlock()

	b.cleanup(aj)

	b.metrics.IncDispatchJobsFailed()

	reason := ""
	if err != nil {
		reason = err.Error()
	}

	if b.publisher != nil {
		b.publisher.PublishTaskFail(aj.job.JobID, reason)
	}

	b.logger.Warn("executor bridge: job failed",
		"job_id", aj.job.JobID,
		"err", reason,
	)
}

// cleanup releases resources and closes the worktree, then removes the
// job from the active map. Safe to call once per job (guarded by the
// finished flag set by completeJob/failJob). All cleanup operations are
// best-effort — errors are logged, not propagated.
func (b *ExecutorBridge) cleanup(aj *activeJob) {
	// Release all Ensured resource refs. Each Release is idempotent on
	// the underlying store.
	for _, ref := range aj.resourceRefs {
		if b.resources != nil {
			b.resources.Release(ref)
		}
	}

	// Close the worktree.
	if aj.worktreePath != "" && b.workspaces != nil {
		if err := b.workspaces.Close(aj.worktreePath); err != nil {
			b.logger.Debug("executor bridge: worktree close failed",
				"job_id", aj.job.JobID,
				"path", aj.worktreePath,
				"err", err,
			)
		}
	}

	// Remove from active map.
	b.mu.Lock()
	// Only delete if it's still us — defends against the extremely
	// unlikely case where a finished job was somehow replaced.
	if current, ok := b.active[aj.job.JobID]; ok && current == aj {
		delete(b.active, aj.job.JobID)
	}
	b.mu.Unlock()
}

// snapshotWorkspace produces a cluster.WorkspaceRef from the job's
// materialized worktree for inclusion in the DispatchResult. Returns
// nil when no workspace was materialized.
//
// In this phase we do not commit changes or compute a new diff — the
// receiver-side worktree state is ephemeral. The returned ref is the
// original job.Workspace. A later phase (orchestrator policy, spec §5
// Phase 5 note) will handle workspace promotion.
func (b *ExecutorBridge) snapshotWorkspace(aj *activeJob) *WorkspaceRef {
	if aj.job.Workspace == nil {
		return nil
	}
	ws := *aj.job.Workspace
	return &ws
}

// --- DispatchExecutor interface implementation ---
//
// ExecutorBridge implements DispatchExecutor (defined in grpc_types.go)
// so the daemon can inject a single object into GRPCTransport via
// SetExecutorBridge. The gRPC DispatchService.Submit handler then routes
// to HandleTaskCreate (for TASK_CREATE-shaped envelopes) or directly to
// the job lifecycle.

// SubmitJob implements DispatchExecutor.SubmitJob. It wraps the job in
// a synthetic ClusterEvent payload and calls HandleTaskCreate so the
// code path is identical whether the job arrives via gRPC or gossip.
func (b *ExecutorBridge) SubmitJob(ctx context.Context, job DispatchJob) (DispatchJobAck, error) {
	payload, err := encodeDispatchJob(job)
	if err != nil {
		return DispatchJobAck{JobID: job.JobID, Accepted: false, Message: err.Error()}, nil
	}

	event := &models.ClusterEvent{
		EventID:   id.Generate("evt-"),
		NodeID:    job.OriginNode,
		EventType: models.EventTaskCreate,
		Timestamp: time.Now(),
		Payload:   payload,
	}

	if err := b.HandleTaskCreate(event); err != nil {
		return DispatchJobAck{JobID: job.JobID, Accepted: false, Message: err.Error()}, nil
	}

	return DispatchJobAck{JobID: job.JobID, Accepted: true}, nil
}

// JobStatus implements DispatchExecutor.JobStatus. It reports the
// runtime state of a job known to this bridge. Unknown job IDs return
// "unknown".
func (b *ExecutorBridge) JobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	b.mu.Lock()
	aj, ok := b.active[jobID]
	b.mu.Unlock()

	if !ok {
		return JobStatus{
			JobID: jobID,
			State: "unknown",
		}, nil
	}

	state := "running"
	if aj.finished {
		state = "completed"
	}

	return JobStatus{
		JobID:     jobID,
		State:     state,
		StartedAt: aj.startedAt.UnixNano(),
		UpdatedAt: time.Now().UnixNano(),
	}, nil
}

// JobResults implements DispatchExecutor.JobResults. In this phase the
// bridge does not retain results after completion (the caller is
// expected to fetch via the TASK_COMPLETE event). Unknown or
// in-progress jobs return an empty slice.
func (b *ExecutorBridge) JobResults(ctx context.Context, jobID string) ([]DispatchResult, error) {
	return nil, nil
}

// Compile-time assertions.
var (
	_ DispatchExecutor = (*ExecutorBridge)(nil)
)

// --- Payload encoding helpers ---
//
// Two payload forms are supported (spec §5 Phase 2):
//  1. Bare TaskPayload (legacy): {"task_id": "...", "agent_id": "...", ...}
//  2. Wrapped: {"dispatch_job": {full DispatchJob envelope}}
//
// decodeDispatchJob sniffs for a "dispatch_job" field; if absent it
// decodes as a bare TaskPayload and synthesizes a DispatchJob.

// wrappedPayload is the envelope form carrying a full DispatchJob.
type wrappedPayload struct {
	DispatchJob *DispatchJob `json:"dispatch_job,omitempty"`
}

// decodeDispatchJob inspects the payload and returns a DispatchJob.
// originNode is the node that emitted the event (used to populate
// OriginNode when the bare TaskPayload form is used).
func decodeDispatchJob(payload json.RawMessage, originNode string) (DispatchJob, error) {
	// Sniff for the wrapped form.
	var wp wrappedPayload
	if err := json.Unmarshal(payload, &wp); err == nil && wp.DispatchJob != nil {
		job := *wp.DispatchJob
		// Populate OriginNode from the event source when the wrapped
		// envelope doesn't carry it (defensive — matches the bare-form
		// behavior below).
		if job.OriginNode == "" && originNode != "" {
			job.OriginNode = originNode
		}
		return job, nil
	}

	// Bare TaskPayload form.
	var tp models.TaskPayload
	if err := json.Unmarshal(payload, &tp); err != nil {
		return DispatchJob{}, fmt.Errorf("unmarshal task payload: %w", err)
	}

	job := DispatchJob{
		JobID:           "dispatch-" + tp.TaskID,
		OriginNode:      originNode,
		AgentID:         tp.AgentID,
		TaskDescription: tp.Description,
		Priority:        tp.Priority,
	}
	if tp.CreatedBy != "" && originNode == "" {
		job.OriginNode = tp.CreatedBy
	}
	// RequiredResources and Workspace are empty for legacy payloads.
	return job, nil
}

// encodeDispatchJob wraps a DispatchJob in the wrappedPayload envelope
// for the synthetic event path (SubmitJob).
func encodeDispatchJob(job DispatchJob) (json.RawMessage, error) {
	wp := wrappedPayload{DispatchJob: &job}
	b, err := json.Marshal(wp)
	if err != nil {
		return nil, fmt.Errorf("marshal dispatch job: %w", err)
	}
	return json.RawMessage(b), nil
}

// writeTempOutput writes the textual output to a temporary file and
// returns its path. The caller invokes ResourceManager.Add synchronously
// after this returns, then removes the file. We do NOT auto-remove the
// file from this helper since Add needs to read it.
func writeTempOutput(output string) (string, error) {
	f, err := os.CreateTemp("", "meept-output-*")
	if err != nil {
		return "", fmt.Errorf("create temp output: %w", err)
	}
	if _, err := f.Write([]byte(output)); err != nil {
		f.Close()
		return "", fmt.Errorf("write temp output: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close temp output: %w", err)
	}
	return f.Name(), nil
}
