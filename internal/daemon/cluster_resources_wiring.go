package daemon

// cluster_resources_wiring.go — Phase 6 wiring for the cluster resource model
// (spec 2026-07-01 §3.2).
//
// This file constructs and wires the cluster resource model components
// (ResourceManager, WorkspaceManager, ExecutorBridge, GRPCTransport,
// PlacementScheduler, DispatchHandler) into Components. It is called from
// daemon.go after the existing cluster wiring block so ClusterConfig,
// ClusterEngine, and ClusterQueue are already set.
//
// Nil-safe: returns nil immediately if ClusterConfig is nil or cluster
// disabled. All dependencies are wired via nil-guarded setters.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/placement"
	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/workspace"
)

// wireClusterResources constructs and wires the cluster resource model
// components (spec §3.2). Called from daemon.go after the existing cluster
// wiring so ClusterConfig, ClusterEngine, and ClusterQueue are already set.
//
// Nil-safe: returns nil immediately if ClusterConfig is nil or cluster
// disabled.
func (c *Components) wireClusterResources(ctx context.Context) error {
	if c.ClusterConfig == nil {
		return nil
	}
	if c.Config == nil || !c.Config.Cluster.Enabled {
		return nil
	}

	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	nodeID := c.ClusterConfig.NodeID

	// --- 1. ResourceManager (spec §4.1) ---
	// Build CAS config from schema, falling back to defaults.
	casCfg := resources.DefaultCASConfig()
	if resCfg := c.Config.Cluster.Resources; resCfg.CASStoreDir != "" {
		casCfg.StoreDir = resCfg.CASStoreDir
	}
	if resCfg := c.Config.Cluster.Resources; resCfg.CASCapacityBytes > 0 {
		casCfg.CapacityBytes = resCfg.CASCapacityBytes
	}
	if resCfg := c.Config.Cluster.Resources; resCfg.EvictionSweepInterval > 0 {
		casCfg.EvictionSweepInterval = resCfg.EvictionSweepInterval
	}
	if resCfg := c.Config.Cluster.Resources; len(resCfg.PinnedHashes) > 0 {
		casCfg.PinnedHashes = resCfg.PinnedHashes
	}
	if resCfg := c.Config.Cluster.Resources; resCfg.HashAlgorithm != "" {
		casCfg.HashAlgorithm = resCfg.HashAlgorithm
	}
	store, err := resources.NewCASStore(casCfg, logger.With("component", "cas_store"))
	if err != nil {
		return fmt.Errorf("wireClusterResources: CAS store: %w", err)
	}
	c.ResourceManager = resources.NewManager(store, logger.With("component", "resource_manager"))

	// Wire metrics adapter (spec §8).
	if c.ClusterMetrics != nil {
		c.ResourceManager.Store().SetMetricsEmitter(&casMetricsAdapter{m: c.ClusterMetrics})
	}

	// --- 2. WorkspaceManager (spec §4.2) ---
	wsCfg := workspace.DefaultConfig()
	if wsCfgSchema := c.Config.Cluster.Workspace; wsCfgSchema.WorktreeRoot != "" {
		wsCfg.WorktreeRoot = wsCfgSchema.WorktreeRoot
	}
	wsCfg.GitFallbackToPeer = c.Config.Cluster.Workspace.GitFallbackToPeer
	wsManager := workspace.NewManager(wsCfg,
		workspace.WithLogger(logger.With("component", "workspace_manager")),
		workspace.WithPatchStore(&patchStoreAdapter{rm: c.ResourceManager}),
		workspace.WithPatchResolver(&patchResolverAdapter{rm: c.ResourceManager}),
	)
	c.WorkspaceManager = wsManager

	// --- 3. ExecutorBridge (spec §4.4) ---
	c.ExecutorBridge = cluster.NewExecutorBridge(nodeID, logger.With("component", "executor_bridge"))
	if c.ClusterMetrics != nil {
		c.ExecutorBridge.SetMetrics(c.ClusterMetrics)
	}
	c.ExecutorBridge.SetResourceManager(c.ResourceManager)
	c.ExecutorBridge.SetWorkspaceManager(c.WorkspaceManager)

	// AgentInvoker adapter wrapping the agent loop.
	if c.AgentLoop != nil {
		c.ExecutorBridge.SetAgentInvoker(&agentInvokerAdapter{loop: c.AgentLoop, logger: logger})
	}

	// BusPublisher adapter wrapping the ClusterEngine.
	if c.ClusterEngine != nil {
		c.ExecutorBridge.SetBusPublisher(&busPublisherAdapter{engine: c.ClusterEngine})
	}

	// Wire ExecutorBridge into the gossip handler (spec §3.2:
	// "case models.EventTaskCreate: return b.executorBridge.HandleTaskCreate(event)").
	if c.GossipHandler != nil {
		if setter, ok := c.GossipHandler.(interface{ SetExecutorBridge(*cluster.ExecutorBridge) }); ok {
			setter.SetExecutorBridge(c.ExecutorBridge)
		} else {
			logger.Warn("wireClusterResources: gossip handler does not expose SetExecutorBridge")
		}
	}

	// --- 4. GRPCTransport (spec §4.3) ---
	c.GRPCTransport = cluster.NewGRPCTransport(c.ClusterConfig, nodeID, logger.With("component", "grpc_transport"))
	c.GRPCTransport.SetResourceManager(&resourceProviderAdapter{rm: c.ResourceManager})
	c.GRPCTransport.SetWorkspaceManager(&workspaceProviderAdapter{wm: c.WorkspaceManager})
	c.GRPCTransport.SetExecutorBridge(c.ExecutorBridge)
	if c.ClusterEngine != nil {
		c.GRPCTransport.SetEventPublisher(c.ClusterEngine)
	}

	// Wire PeerFetcher into ResourceManager so CAS misses trigger gRPC fetch.
	c.ResourceManager.SetPeerFetcher(&peerFetcherAdapter{
		transport: c.GRPCTransport,
		store:     store,
		logger:    logger,
	})

	// --- 5. PlacementScheduler (spec §4.5) ---
	c.PlacementScheduler = placement.NewPlacementScheduler(nodeID, logger.With("component", "placement_scheduler"))
	if dCfg := c.Config.Cluster.Dispatch; dCfg.SchedulerNoCapacityPolicy != "" {
		c.PlacementScheduler.SetPolicy(dCfg.SchedulerNoCapacityPolicy)
	} else {
		c.PlacementScheduler.SetPolicy(placement.PolicyQueue)
	}
	if dCfg := c.Config.Cluster.Dispatch; dCfg.PeerFallbackPolicy != "" {
		c.PlacementScheduler.SetFallback(dCfg.PeerFallbackPolicy)
	} else {
		c.PlacementScheduler.SetFallback(placement.FallbackIfCapacity)
	}

	// --- 6. DispatchHandler (spec §2.3 γ) ---
	dispatchSubmitter := &dispatchSubmitterAdapter{
		transport:   c.GRPCTransport,
		localNodeID: nodeID,
		logger:      logger,
	}
	c.DispatchHandler = rpc.NewDispatchHandler(dispatchSubmitter, logger.With("component", "dispatch_handler"))
	c.setDispatchSubmitter(dispatchSubmitter)

	logger.Info("cluster resource model wired",
		"node_id", nodeID,
		"cas_dir", casCfg.StoreDir,
		"worktree_root", wsCfg.WorktreeRoot,
	)
	return nil
}

// --- Adapters ---

// agentInvokerAdapter wraps *agent.AgentLoop to satisfy cluster.AgentInvoker.
type agentInvokerAdapter struct {
	loop   *agent.AgentLoop
	logger *slog.Logger
}

func (a *agentInvokerAdapter) InvokeTask(ctx context.Context, job cluster.DispatchJob, worktreePath string) (string, []string, error) {
	if a.loop == nil {
		return "", nil, fmt.Errorf("agent loop not configured")
	}
	convID := "dispatch-" + job.JobID
	output, err := a.loop.RunOnce(ctx, job.TaskDescription, convID)
	if err != nil {
		return "", nil, err
	}
	return output, nil, nil
}

// busPublisherAdapter wraps *cluster.GossipEngine to satisfy cluster.BusPublisher.
type busPublisherAdapter struct {
	engine *cluster.GossipEngine
}

func (b *busPublisherAdapter) PublishTaskComplete(jobID, outputRef string, ws *cluster.WorkspaceRef) {
	if b.engine == nil {
		return
	}
	payload := map[string]any{
		"job_id":     jobID,
		"output_ref": outputRef,
		"state":      "completed",
	}
	if ws != nil {
		payload["workspace"] = ws
	}
	_ = b.engine.PublishClusterEvent("TASK_COMPLETE", payload)
}

func (b *busPublisherAdapter) PublishTaskFail(jobID, reason string) {
	if b.engine == nil {
		return
	}
	_ = b.engine.PublishClusterEvent("TASK_FAIL", map[string]any{
		"job_id": jobID,
		"reason": reason,
		"state":  "failed",
	})
}

// resourceProviderAdapter wraps *resources.Manager to satisfy
// cluster.ResourceProvider for the gRPC server handlers.
type resourceProviderAdapter struct {
	rm *resources.Manager
}

func (a *resourceProviderAdapter) Has(hash string) bool {
	if a.rm == nil {
		return false
	}
	return a.rm.Has(hash)
}

func (a *resourceProviderAdapter) GetPath(hash string) (string, error) {
	if a.rm == nil {
		return "", fmt.Errorf("resource manager not configured")
	}
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return a.rm.Store().GetPath(body)
	}
	return a.rm.Store().GetPath(hash)
}

func (a *resourceProviderAdapter) Stat(hash string) (size int64, addedAt time.Time, source string, pinned bool, refcount int, err error) {
	if a.rm == nil {
		err = fmt.Errorf("resource manager not configured")
		return
	}
	_, body, isCAS := resources.ParseRef(hash)
	if !isCAS {
		body = hash
	}
	path, err := a.rm.Store().GetPath(body)
	if err != nil {
		return
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		err = statErr
		return
	}
	size = info.Size()
	addedAt = info.ModTime()
	source = "local"
	refcount = a.rm.Store().Refcount(body)
	pinned = a.rm.Store().IsPinned(body)
	return
}

// workspaceProviderAdapter wraps *workspace.Manager to satisfy
// cluster.WorkspaceProvider.
type workspaceProviderAdapter struct {
	wm *workspace.Manager
}

func (a *workspaceProviderAdapter) Ensure(ctx context.Context, ref cluster.WorkspaceRef) (string, error) {
	if a.wm == nil {
		return "", fmt.Errorf("workspace manager not configured")
	}
	wsRef := workspace.WorkspaceRef{
		RepoURL:      ref.RepoURL,
		CommitSHA:    ref.CommitSHA,
		DiffBlobHash: ref.DiffBlobHash,
		Dirty:        ref.Dirty,
	}
	return a.wm.Ensure(ctx, wsRef)
}

// peerFetcherAdapter wraps *cluster.GRPCTransport + *resources.CASStore to
// satisfy resources.PeerFetcher. On CAS miss, it queries known peers for the
// blob and streams it via Fetch.
type peerFetcherAdapter struct {
	transport *cluster.GRPCTransport
	store     *resources.CASStore
	logger    *slog.Logger
}

func (a *peerFetcherAdapter) Fetch(ctx context.Context, hashHex, algo string) (string, error) {
	if a.transport == nil || a.store == nil {
		return "", fmt.Errorf("resources: peer fetcher not fully configured")
	}

	peers := a.transport.PeerList()
	if len(peers) == 0 {
		return "", fmt.Errorf("resources: no peers available for fetch")
	}

	ref := resources.HashPrefix(algo, hashHex)
	for _, nodeID := range peers {
		pc, err := a.transport.DialPeer(ctx, nodeID, "")
		if err != nil {
			a.logger.Debug("resources: fetch: dial peer failed", "peer", nodeID, "err", err)
			continue
		}
		resp, err := pc.Has(ctx, ref)
		if err != nil || !resp.Has {
			continue
		}
		// Stream the blob.
		reader, _, err := pc.Fetch(ctx, ref, 0)
		if err != nil {
			a.logger.Debug("resources: fetch: stream failed", "peer", nodeID, "err", err)
			continue
		}

		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			continue
		}

		// Store via CAS StoreBlob (refcount=0; caller calls IncrementRef).
		if err := a.store.StoreBlob(ctx, hashHex, data, "peer-"+nodeID); err != nil {
			a.logger.Warn("resources: fetch: store blob failed", "hash", hashHex, "err", err)
			continue
		}
		path, err := a.store.GetPath(hashHex)
		if err != nil {
			continue
		}
		return path, nil
	}
	return "", fmt.Errorf("resources: no peer has blob %s", ref)
}

// dispatchSubmitterAdapter wraps *cluster.GRPCTransport to satisfy
// rpc.DispatchSubmitter.
type dispatchSubmitterAdapter struct {
	transport   *cluster.GRPCTransport
	localNodeID string
	logger      *slog.Logger
}

func (d *dispatchSubmitterAdapter) Submit(ctx context.Context, req rpc.DispatchJobRequest) (rpc.DispatchJobAck, error) {
	if d.transport == nil {
		return rpc.DispatchJobAck{}, fmt.Errorf("dispatch feature not enabled: no gRPC transport")
	}

	pc, err := d.transport.DialPeer(ctx, req.TargetNode, "")
	if err != nil {
		return rpc.DispatchJobAck{}, fmt.Errorf("dispatch: dial target node %s: %w", req.TargetNode, err)
	}

	// Translate rpc.WorkspaceRef → cluster.WorkspaceRef.
	var wsRef *cluster.WorkspaceRef
	if req.Workspace != nil {
		wsRef = &cluster.WorkspaceRef{
			RepoURL:      req.Workspace.RepoURL,
			CommitSHA:    req.Workspace.CommitSHA,
			DiffBlobHash: req.Workspace.DiffBlobHash,
			Dirty:        req.Workspace.Dirty,
		}
	}

	job := cluster.DispatchJob{
		JobID:             fmt.Sprintf("dispatch-%d", time.Now().UnixNano()),
		OriginNode:        d.localNodeID,
		TargetNode:        req.TargetNode,
		AgentID:           req.AgentID,
		TaskDescription:   req.TaskDescription,
		RequiredResources: req.RequiredResources,
		Workspace:         wsRef,
		Priority:          req.Priority,
		CreatedAt:         time.Now().UnixNano(),
	}

	ack, err := pc.Submit(ctx, job)
	if err != nil {
		return rpc.DispatchJobAck{}, fmt.Errorf("dispatch: submit to %s: %w", req.TargetNode, err)
	}
	return rpc.DispatchJobAck{
		JobID:    ack.JobID,
		Accepted: ack.Accepted,
		Message:  ack.Message,
	}, nil
}

func (d *dispatchSubmitterAdapter) Status(ctx context.Context, jobID string) (rpc.JobStatus, error) {
	return rpc.JobStatus{JobID: jobID, State: "unknown"}, nil
}

func (d *dispatchSubmitterAdapter) Results(ctx context.Context, jobID string) ([]rpc.DispatchResult, error) {
	return []rpc.DispatchResult{}, nil
}

// patchStoreAdapter bridges workspace.PatchStore to resources.Manager.
type patchStoreAdapter struct {
	rm *resources.Manager
}

func (a *patchStoreAdapter) Add(ctx context.Context, srcPath string) (string, error) {
	if a.rm == nil {
		return "", fmt.Errorf("resource manager not configured")
	}
	return a.rm.Add(ctx, srcPath)
}

func (a *patchStoreAdapter) Resolve(hash string) (string, error) {
	if a.rm == nil {
		return "", fmt.Errorf("resource manager not configured")
	}
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return a.rm.Store().GetPath(body)
	}
	return a.rm.Store().GetPath(hash)
}

// patchResolverAdapter bridges workspace.PatchResolver to resources.Manager.
type patchResolverAdapter struct {
	rm *resources.Manager
}

func (a *patchResolverAdapter) Resolve(hash string) (string, error) {
	if a.rm == nil {
		return "", fmt.Errorf("resource manager not configured")
	}
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return a.rm.Store().GetPath(body)
	}
	return a.rm.Store().GetPath(hash)
}

// casMetricsAdapter bridges resources.MetricsEmitter to cluster.Metrics.
type casMetricsAdapter struct {
	m *cluster.Metrics
}

func (a *casMetricsAdapter) IncCASHits()                 { a.m.IncCASHits() }
func (a *casMetricsAdapter) IncCASMisses()               { a.m.IncCASMisses() }
func (a *casMetricsAdapter) IncCASBytesFetched(n int64)  { a.m.AddCASBytesFetched(n) }
func (a *casMetricsAdapter) IncCASBytesEvicted(n int64)  { a.m.AddCASBytesEvicted(n) }
func (a *casMetricsAdapter) IncCASRefcountZeroEligible() { a.m.IncCASRefcountZeroEligible() }
