package daemon

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// TestWireClusterResources_NilSafe verifies that wireClusterResources is
// nil-safe when cluster config or the components struct is not fully wired.
func TestWireClusterResources_NilSafe(t *testing.T) {
	t.Parallel()

	t.Run("nil cluster config", func(t *testing.T) {
		c := &Components{}
		if err := c.wireClusterResources(context.Background()); err != nil {
			t.Fatalf("expected nil error with nil cluster config, got: %v", err)
		}
		if c.ResourceManager != nil {
			t.Error("expected nil ResourceManager with nil cluster config")
		}
	})

	t.Run("cluster disabled", func(t *testing.T) {
		c := &Components{
			ClusterConfig: &cluster.Config{NodeID: "test-node"},
			Config: &config.Config{
				Cluster: config.ClusterConfig{Enabled: false},
			},
		}
		if err := c.wireClusterResources(context.Background()); err != nil {
			t.Fatalf("expected nil error with cluster disabled, got: %v", err)
		}
		if c.ResourceManager != nil {
			t.Error("expected nil ResourceManager with cluster disabled")
		}
	})
}

// TestWireClusterResources_Wiring verifies that wireClusterResources constructs
// and wires all components when cluster mode is enabled.
func TestWireClusterResources_Wiring(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	c := &Components{
		ClusterConfig: &cluster.Config{NodeID: "test-node-wiring"},
		Config: &config.Config{
			Cluster: config.ClusterConfig{Enabled: true},
		},
		ClusterMetrics: cluster.NewMetrics(),
	}

	if err := c.wireClusterResources(context.Background()); err != nil {
		t.Fatalf("wireClusterResources failed: %v", err)
	}

	// Verify all components are constructed.
	if c.ResourceManager == nil {
		t.Fatal("expected ResourceManager to be wired")
	}
	if c.WorkspaceManager == nil {
		t.Fatal("expected WorkspaceManager to be wired")
	}
	if c.ExecutorBridge == nil {
		t.Fatal("expected ExecutorBridge to be wired")
	}
	if c.GRPCTransport == nil {
		t.Fatal("expected GRPCTransport to be wired")
	}
	if c.PlacementScheduler == nil {
		t.Fatal("expected PlacementScheduler to be wired")
	}
	if c.DispatchHandler == nil {
		t.Fatal("expected DispatchHandler to be wired")
	}

	// Verify dispatch submitter is wired.
	ds := c.getDispatchSubmitter()
	if ds == nil {
		t.Fatal("expected dispatch submitter to be wired")
	}

	// Verify the dispatch submitter returns an error when submitting to
	// a nonexistent node (transport not started, no peers registered).
	_, err := ds.Submit(context.Background(), rpc.DispatchJobRequest{
		TargetNode:      "nonexistent",
		AgentID:         "coder",
		TaskDescription: "test task",
	})
	if err == nil {
		t.Error("expected error submitting to nonexistent node before transport start")
	}

	// Clean up CAS store.
	if c.ResourceManager != nil && c.ResourceManager.Store() != nil {
		_ = c.ResourceManager.Store().Close()
	}
}

// TestWireClusterResources_GossipHandler verifies the ExecutorBridge wiring
// works correctly with a gossip handler.
func TestWireClusterResources_GossipHandler(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	mock := &mockGossipHandler{}
	c := &Components{
		ClusterConfig: &cluster.Config{NodeID: "test-node-gossip"},
		Config: &config.Config{
			Cluster: config.ClusterConfig{Enabled: true},
		},
		ClusterMetrics: cluster.NewMetrics(),
		GossipHandler:  mock,
	}

	if err := c.wireClusterResources(context.Background()); err != nil {
		t.Fatalf("wireClusterResources failed: %v", err)
	}

	if c.ExecutorBridge == nil {
		t.Fatal("expected ExecutorBridge to be wired")
	}
	if !mock.bridgeSet {
		t.Error("expected gossip handler to have ExecutorBridge set")
	}

	// Clean up CAS store.
	if c.ResourceManager != nil && c.ResourceManager.Store() != nil {
		_ = c.ResourceManager.Store().Close()
	}
}

// mockGossipHandler implements both GossipHandler and SetExecutorBridge.
type mockGossipHandler struct {
	bridgeSet bool
}

func (m *mockGossipHandler) OnEvent(event *models.ClusterEvent) error {
	return nil
}

func (m *mockGossipHandler) SetExecutorBridge(b *cluster.ExecutorBridge) {
	if b != nil {
		m.bridgeSet = true
	}
}

// TestNodePrefixRouting verifies that the node: prefix is parsed correctly
// for team.assign routing (spec §2.3 α).
func TestNodePrefixRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		agentID     string
		wantNodeID  string
		wantAgentID string
		wantOK      bool
	}{
		{"node:abc-123:coder", "abc-123", "coder", true},
		{"node:peer1:analyst", "peer1", "analyst", true},
		{"node:single", "single", "", true},
		{"regular-agent", "", "", false},
		{"", "", "", false},
	}

	for _, tt := range tests {
		nodeID, agentID, ok := rpc.SplitNodePrefixedAgentID(tt.agentID)
		if ok != tt.wantOK {
			t.Errorf("SplitNodePrefixedAgentID(%q): ok=%v, want %v", tt.agentID, ok, tt.wantOK)
			continue
		}
		if nodeID != tt.wantNodeID {
			t.Errorf("SplitNodePrefixedAgentID(%q): nodeID=%q, want %q", tt.agentID, nodeID, tt.wantNodeID)
		}
		if agentID != tt.wantAgentID {
			t.Errorf("SplitNodePrefixedAgentID(%q): agentID=%q, want %q", tt.agentID, agentID, tt.wantAgentID)
		}
	}
}

// TestDispatchSubmitterHolder verifies the get/set pattern for the dispatch
// submitter on Components.
func TestDispatchSubmitterHolder(t *testing.T) {
	t.Parallel()

	c := &Components{}

	// Initially nil.
	if ds := c.getDispatchSubmitter(); ds != nil {
		t.Fatal("expected nil dispatch submitter initially")
	}

	// Set a mock submitter.
	mock := &mockDispatchSubmitter{}
	c.setDispatchSubmitter(mock)

	// Verify it's retrievable.
	ds := c.getDispatchSubmitter()
	if ds == nil {
		t.Fatal("expected non-nil dispatch submitter after set")
	}

	// Verify it works.
	ack, err := ds.Submit(context.Background(), rpc.DispatchJobRequest{
		TargetNode:      "test",
		AgentID:         "coder",
		TaskDescription: "test",
	})
	if err != nil {
		t.Errorf("unexpected error from mock submitter: %v", err)
	}
	if !ack.Accepted {
		t.Error("expected ack.Accepted=true from mock submitter")
	}
}

// mockDispatchSubmitter implements rpc.DispatchSubmitter for testing.
type mockDispatchSubmitter struct{}

func (m *mockDispatchSubmitter) Submit(ctx context.Context, req rpc.DispatchJobRequest) (rpc.DispatchJobAck, error) {
	return rpc.DispatchJobAck{
		JobID:    id.Generate("mock-"),
		Accepted: true,
	}, nil
}

func (m *mockDispatchSubmitter) Status(ctx context.Context, jobID string) (rpc.JobStatus, error) {
	return rpc.JobStatus{JobID: jobID, State: "completed"}, nil
}

func (m *mockDispatchSubmitter) Results(ctx context.Context, jobID string) ([]rpc.DispatchResult, error) {
	return []rpc.DispatchResult{}, nil
}
