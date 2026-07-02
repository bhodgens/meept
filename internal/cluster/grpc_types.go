package cluster

// grpc_types.go — Go structs mirroring proto/cluster.proto messages.
//
// These types are used by the manual gRPC service registration
// (grpc_transport.go) with a JSON codec, avoiding the need for protoc.
// When a protoc toolchain is introduced, generated types will replace these.
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §4.3

import (
	"context"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// ----- Shared -----

// Ack is the generic acknowledgment for EventService.
type Ack struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message"`
}

// ----- ResourceService -----

// HasRequest asks whether the peer has a blob by hash.
type HasRequest struct {
	Hash string `json:"hash"`
}

// HasResponse is the reply to HasRequest.
type HasResponse struct {
	Has bool `json:"has"`
}

// FetchRequest requests blob content starting at the given byte offset.
type FetchRequest struct {
	Hash   string `json:"hash"`
	Offset int64  `json:"offset"`
}

// FetchChunk carries one chunk of blob data in a Fetch stream.
type FetchChunk struct {
	Data      []byte `json:"data"`
	Offset    int64  `json:"offset"`
	TotalSize int64  `json:"total_size"`
	Hash      string `json:"hash"`
}

// StatRequest requests metadata about a CAS blob.
type StatRequest struct {
	Hash string `json:"hash"`
}

// StatResponse returns CAS blob metadata.
type StatResponse struct {
	Size     int64  `json:"size"`
	AddedAt  int64  `json:"added_at"` // unix nanoseconds
	Source   string `json:"source"`
	Pinned   bool   `json:"pinned"`
	Refcount int    `json:"refcount"`
}

// ----- WorkspaceService -----

// WorkspaceRef captures source-tree state at dispatch time. Matches
// internal/workspace.WorkspaceRef from the spec (§4.2) but kept local to
// avoid an import cycle — the daemon writes an adapter.
type WorkspaceRef struct {
	RepoURL      string `json:"repo_url"`
	CommitSHA    string `json:"commit_sha"`
	DiffBlobHash string `json:"diff_blob_hash"`
	Dirty        bool   `json:"dirty"`
}

// WorkspaceReady is the reply to Prepare.
type WorkspaceReady struct {
	WorktreePath string `json:"worktree_path"`
	Ready        bool   `json:"ready"`
	Error        string `json:"error"`
}

// GitObjectRequest requests a specific git object from a peer.
type GitObjectRequest struct {
	RepoURL    string `json:"repo_url"`
	CommitSHA  string `json:"commit_sha"`
	ObjectType string `json:"object_type"`
}

// GitObject carries a single git object in a GitFetch stream.
type GitObject struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
	Hash string `json:"hash"`
}

// ----- DispatchService -----

// DispatchJob is the envelope for cross-daemon task dispatch (spec §5 Phase 1).
type DispatchJob struct {
	JobID             string        `json:"job_id"`
	OriginNode        string        `json:"origin_node"`
	TargetNode        string        `json:"target_node"`
	AgentID           string        `json:"agent_id"`
	TaskDescription   string        `json:"task_description"`
	RequiredResources []string      `json:"required_resources"`
	Workspace         *WorkspaceRef `json:"workspace,omitempty"`
	Priority          int           `json:"priority"`
	CreatedAt         int64         `json:"created_at"` // unix nanoseconds
	Signature         []byte        `json:"signature"`
}

// DispatchJobAck acknowledges a submitted dispatch job.
type DispatchJobAck struct {
	JobID    string `json:"job_id"`
	Accepted bool   `json:"accepted"`
	Message  string `json:"message"`
}

// JobID wraps a job identifier for Status and Results RPCs.
type JobID struct {
	ID string `json:"id"`
}

// JobStatus is the current state of a dispatch job.
type JobStatus struct {
	JobID     string `json:"job_id"`
	State     string `json:"state"` // "queued" | "running" | "completed" | "failed"
	StartedAt int64  `json:"started_at"`
	UpdatedAt int64  `json:"updated_at"`
	Error     string `json:"error"`
}

// DispatchResult carries completion details for a dispatch job.
type DispatchResult struct {
	JobID       string        `json:"job_id"`
	OutputRef   string        `json:"output_ref"`
	Workspace   *WorkspaceRef `json:"workspace,omitempty"`
	Error       string        `json:"error"`
	CompletedAt int64         `json:"completed_at"`
}

// ----- Provider interfaces (injected via setters on GRPCTransport) -----
//
// These are package-local so daemon wiring writes adapters. Keeping them
// minimal prevents importing internal/resources or internal/workspace.

// ResourceProvider backs ResourceService handler operations.
type ResourceProvider interface {
	// Has returns true if the blob is in the local CAS store.
	Has(hash string) bool
	// GetPath returns the local filesystem path for the blob's data file.
	GetPath(hash string) (string, error)
	// Stat returns metadata for the blob.
	Stat(hash string) (size int64, addedAt time.Time, source string, pinned bool, refcount int, err error)
}

// WorkspaceProvider backs WorkspaceService.Prepare.
type WorkspaceProvider interface {
	// Ensure materializes the workspace and returns the local worktree path.
	Ensure(ctx context.Context, ref WorkspaceRef) (string, error)
}

// DispatchExecutor backs DispatchService handler operations.
type DispatchExecutor interface {
	// SubmitJob accepts a dispatch job for local execution.
	SubmitJob(ctx context.Context, job DispatchJob) (DispatchJobAck, error)
	// JobStatus returns the current state of a job.
	JobStatus(ctx context.Context, jobID string) (JobStatus, error)
	// JobResults returns completion results for a finished job.
	JobResults(ctx context.Context, jobID string) ([]DispatchResult, error)
}

// EventPublisher backs EventService handler operations.
// GossipEngine already implements PublishClusterEvent.
type EventPublisher interface {
	PublishClusterEvent(eventType models.ClusterEventType, payload any) error
}

// ----- ClusterEvent compatibility -----
//
// For EventService, we use pkg/models.ClusterEvent directly since it is the
// canonical type throughout the codebase. No wrapper needed.

// Compile-time assertion that models.ClusterEvent can be used as the
// EventService message type.
var _ = (*models.ClusterEvent)(nil)

// ----- Errors -----
//
// Error variables are defined in grpc_handlers.go where they are used.

// ----- Constants -----

const (
	// fetchChunkSize is the default chunk size for ResourceService.Fetch (1 MiB).
	// Spec §4.3: chunk size 1–4 MiB.
	fetchChunkSize = 1 << 20 // 1 MiB
)
