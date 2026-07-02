package integration

// cluster_helpers.go — Test helpers for cluster resource model integration
// tests (spec §10). Spins up N in-process "daemons" with wired gRPC
// transports, ResourceManagers, WorkspaceManagers, and ExecutorBridges.
// Real files in temp dirs. Only network is in-process gRPC (listeners on
// 127.0.0.1 + OS-assigned ports).

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/resources"
	"github.com/caimlas/meept/internal/workspace"
	"github.com/caimlas/meept/pkg/id"
)

// testDaemon is a minimal in-process daemon for cluster resource model
// integration tests. It wires ResourceManager, WorkspaceManager,
// ExecutorBridge, and GRPCTransport with real file I/O.
type testDaemon struct {
	nodeID      string
	tmpDir      string
	casDir      string
	worktreeDir string

	resourceManager  *resources.Manager
	workspaceManager *workspace.Manager
	executorBridge   *cluster.ExecutorBridge
	grpcTransport    *cluster.GRPCTransport
	metrics          *cluster.Metrics

	listenAddr string
	cancel     context.CancelFunc
}

// newTestDaemon constructs a test daemon with isolated temp directories
// and wired cluster resource model components. The gRPC transport is
// started on an OS-assigned port.
func newTestDaemon(t *testing.T, ctx context.Context, nodeID string) *testDaemon {
	t.Helper()

	tmpDir := t.TempDir()
	casDir := filepath.Join(tmpDir, "resources")
	worktreeDir := filepath.Join(tmpDir, "worktrees")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})).With("node", nodeID)

	// CAS store + ResourceManager.
	casCfg := resources.CASConfig{
		StoreDir:              casDir,
		CapacityBytes:         0, // unlimited for tests unless overridden
		EvictionSweepInterval: 0, // disable background sweep
		HashAlgorithm:         resources.AlgoBlake3,
	}
	store, err := resources.NewCASStore(casCfg, logger)
	if err != nil {
		t.Fatalf("newTestDaemon(%s): CAS store: %v", nodeID, err)
	}

	rm := resources.NewManager(store, logger)
	metrics := cluster.NewMetrics()
	store.SetMetricsEmitter(&testCASMetricsAdapter{m: metrics})

	// WorkspaceManager.
	wsCfg := workspace.Config{
		WorktreeRoot:      worktreeDir,
		GitFallbackToPeer: false,
	}
	wm := workspace.NewManager(wsCfg,
		workspace.WithLogger(logger),
		workspace.WithPatchStore(&testPatchStoreAdapter{rm: rm}),
		workspace.WithPatchResolver(&testPatchResolverAdapter{rm: rm}),
	)

	// ExecutorBridge.
	eb := cluster.NewExecutorBridge(nodeID, logger)
	eb.SetMetrics(metrics)
	eb.SetResourceManager(rm)
	eb.SetWorkspaceManager(wm)

	// GRPCTransport.
	clusterCfg := &cluster.Config{
		NodeID:  nodeID,
		ClusterID: "test-cluster",
	}
	transport := cluster.NewGRPCTransport(clusterCfg, nodeID, logger)
	transport.SetResourceManager(&testResourceProviderAdapter{rm: rm})
	transport.SetWorkspaceManager(&testWorkspaceProviderAdapter{wm: wm})
	transport.SetExecutorBridge(eb)

	// Wire peer fetcher.
	rm.SetPeerFetcher(&testPeerFetcherAdapter{
		transport: transport,
		store:     store,
		logger:    logger,
	})

	// Find a free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("newTestDaemon(%s): find port: %v", nodeID, err)
	}
	addr := listener.Addr().String()
	listener.Close()

	daemonCtx, cancel := context.WithCancel(ctx)

	if err := transport.Start(daemonCtx, addr); err != nil {
		cancel()
		t.Fatalf("newTestDaemon(%s): start transport: %v", nodeID, err)
	}

	return &testDaemon{
		nodeID:           nodeID,
		tmpDir:           tmpDir,
		casDir:           casDir,
		worktreeDir:      worktreeDir,
		resourceManager:  rm,
		workspaceManager: wm,
		executorBridge:   eb,
		grpcTransport:    transport,
		metrics:          metrics,
		listenAddr:       addr,
		cancel:           cancel,
	}
}

// Close stops the daemon and cleans up resources.
func (d *testDaemon) Close() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.grpcTransport != nil {
		_ = d.grpcTransport.Stop()
	}
	if d.resourceManager != nil && d.resourceManager.Store() != nil {
		_ = d.resourceManager.Store().Close()
	}
}

// connectPeers registers each daemon's address with the other so they can
// dial each other.
func connectPeers(daemons ...*testDaemon) {
	for _, a := range daemons {
		for _, b := range daemons {
			if a.nodeID == b.nodeID {
				continue
			}
			a.grpcTransport.RegisterPeerAddr(b.nodeID, b.listenAddr)
		}
	}
}

// waitForPeerConnection waits until source can dial target or times out.
func waitForPeerConnection(t *testing.T, source, target *testDaemon, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := source.grpcTransport.DialPeer(context.Background(), target.nodeID, target.listenAddr)
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("waitForPeerConnection: %s could not dial %s within %v", source.nodeID, target.nodeID, timeout)
}

// addFileToCAS adds a file to the daemon's CAS and returns the hash.
func (d *testDaemon) addFileToCAS(t *testing.T, ctx context.Context, content string) string {
	t.Helper()
	path := filepath.Join(d.tmpDir, id.Generate("file-"))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("addFileToCAS: write: %v", err)
	}
	hash, err := d.resourceManager.Add(ctx, path)
	if err != nil {
		t.Fatalf("addFileToCAS: add: %v", err)
	}
	return hash
}

// --- Adapters (simplified copies of the daemon's adapters for test use) ---

type testCASMetricsAdapter struct{ m *cluster.Metrics }

func (a *testCASMetricsAdapter) IncCASHits()                 { a.m.IncCASHits() }
func (a *testCASMetricsAdapter) IncCASMisses()               { a.m.IncCASMisses() }
func (a *testCASMetricsAdapter) IncCASBytesFetched(n int64)  { a.m.AddCASBytesFetched(n) }
func (a *testCASMetricsAdapter) IncCASBytesEvicted(n int64)  { a.m.AddCASBytesEvicted(n) }
func (a *testCASMetricsAdapter) IncCASRefcountZeroEligible() { a.m.IncCASRefcountZeroEligible() }

type testResourceProviderAdapter struct{ rm *resources.Manager }

func (a *testResourceProviderAdapter) Has(hash string) bool {
	return a.rm.Has(hash)
}
func (a *testResourceProviderAdapter) GetPath(hash string) (string, error) {
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return a.rm.Store().GetPath(body)
	}
	return a.rm.Store().GetPath(hash)
}
func (a *testResourceProviderAdapter) Stat(hash string) (size int64, addedAt time.Time, source string, pinned bool, refcount int, err error) {
	_, body, isCAS := resources.ParseRef(hash)
	if !isCAS {
		body = hash
	}
	path, err := a.rm.Store().GetPath(body)
	if err != nil {
		return
	}
	info, e := os.Stat(path)
	if e != nil {
		err = e
		return
	}
	size = info.Size()
	addedAt = info.ModTime()
	source = "local"
	refcount = a.rm.Store().Refcount(body)
	pinned = a.rm.Store().IsPinned(body)
	return
}

type testWorkspaceProviderAdapter struct{ wm *workspace.Manager }

func (a *testWorkspaceProviderAdapter) Ensure(ctx context.Context, ref cluster.WorkspaceRef) (string, error) {
	return a.wm.Ensure(ctx, workspace.WorkspaceRef{
		RepoURL:      ref.RepoURL,
		CommitSHA:    ref.CommitSHA,
		DiffBlobHash: ref.DiffBlobHash,
		Dirty:        ref.Dirty,
	})
}

type testPatchStoreAdapter struct{ rm *resources.Manager }

func (a *testPatchStoreAdapter) Add(ctx context.Context, srcPath string) (string, error) {
	return a.rm.Add(ctx, srcPath)
}
func (a *testPatchStoreAdapter) Resolve(hash string) (string, error) {
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return a.rm.Store().GetPath(body)
	}
	return a.rm.Store().GetPath(hash)
}

type testPatchResolverAdapter struct{ rm *resources.Manager }

func (a *testPatchResolverAdapter) Resolve(hash string) (string, error) {
	_, body, isCAS := resources.ParseRef(hash)
	if isCAS {
		return a.rm.Store().GetPath(body)
	}
	return a.rm.Store().GetPath(hash)
}

type testPeerFetcherAdapter struct {
	transport *cluster.GRPCTransport
	store     *resources.CASStore
	logger    *slog.Logger
}

func (a *testPeerFetcherAdapter) Fetch(ctx context.Context, hashHex, algo string) (string, error) {
	if a.transport == nil || a.store == nil {
		return "", fmt.Errorf("peer fetcher not configured")
	}
	peers := a.transport.PeerList()
	if len(peers) == 0 {
		return "", fmt.Errorf("no peers available")
	}
	ref := resources.HashPrefix(algo, hashHex)
	for _, nodeID := range peers {
		pc, err := a.transport.DialPeer(ctx, nodeID, "")
		if err != nil {
			continue
		}
		resp, err := pc.Has(ctx, ref)
		if err != nil || !resp.Has {
			continue
		}
		reader, _, err := pc.Fetch(ctx, ref, 0)
		if err != nil {
			continue
		}
		data, err := readAllCloser(reader)
		if err != nil {
			continue
		}
		if err := a.store.StoreBlob(ctx, hashHex, data, "peer-"+nodeID); err != nil {
			continue
		}
		return a.store.GetPath(hashHex)
	}
	return "", fmt.Errorf("no peer has blob %s", ref)
}

// readAllCloser reads all bytes from a reader and closes it.
func readAllCloser(r interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer r.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
	}
	return buf, nil
}
