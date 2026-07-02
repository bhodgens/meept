package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// DispatchSubmitter is the interface the daemon implements to route dispatch
// operations to a remote node via gRPC. Phase 6 wires the concrete
// implementation (backed by cluster.GRPCTransport).
type DispatchSubmitter interface {
	// Submit dispatches a job to a remote node. The daemon implements this
	// by calling GRPCTransport.PeerClient.Submit.
	Submit(ctx context.Context, job DispatchJobRequest) (DispatchJobAck, error)
	// Status queries the current state of a previously submitted job.
	Status(ctx context.Context, jobID string) (JobStatus, error)
	// Results fetches the completed results for a finished job.
	Results(ctx context.Context, jobID string) ([]DispatchResult, error)
}

// WorkspaceRef captures source-tree state at dispatch time.
// Redefined here to avoid importing internal/cluster from internal/rpc.
// Mirrors cluster.WorkspaceRef / workspace.WorkspaceRef.
type WorkspaceRef struct {
	RepoURL      string `json:"repo_url,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	DiffBlobHash string `json:"diff_blob_hash,omitempty"`
	Dirty        bool   `json:"dirty,omitempty"`
}

// DispatchJobRequest is the payload for dispatch.submit.
type DispatchJobRequest struct {
	TargetNode        string        `json:"target_node"`
	AgentID           string        `json:"agent_id"`
	TaskDescription   string        `json:"task_description"`
	RequiredResources []string      `json:"required_resources,omitempty"`
	Workspace         *WorkspaceRef `json:"workspace,omitempty"`
	Priority          int           `json:"priority,omitempty"`
}

// DispatchJobAck is the acknowledgement returned by dispatch.submit.
type DispatchJobAck struct {
	JobID    string `json:"job_id"`
	Accepted bool   `json:"accepted"`
	Message  string `json:"message,omitempty"`
}

// JobStatus represents the state of a dispatched job at query time.
type JobStatus struct {
	JobID     string `json:"job_id"`
	State     string `json:"state"`
	StartedAt int64  `json:"started_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// DispatchResult represents the output of a completed dispatch job.
type DispatchResult struct {
	JobID       string        `json:"job_id"`
	OutputRef   string        `json:"output_ref,omitempty"`
	Workspace   *WorkspaceRef `json:"workspace,omitempty"`
	Error       string        `json:"error,omitempty"`
	CompletedAt int64         `json:"completed_at,omitempty"`
}

// DispatchHandler provides RPC methods for cross-daemon task dispatch
// (spec §2.3 γ/β trigger surfaces). If submitter is nil the methods return
// "dispatch feature not enabled" — matching the QueueHandler pattern.
type DispatchHandler struct {
	submitter DispatchSubmitter
	logger    *slog.Logger
}

// NewDispatchHandler creates a new handler. If submitter is nil the registered
// methods return "dispatch feature not enabled" errors.
func NewDispatchHandler(submitter DispatchSubmitter, logger *slog.Logger) *DispatchHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DispatchHandler{submitter: submitter, logger: logger}
}

// SetSubmitter sets the submitter after construction (daemon Phase 6 wiring).
// Nil-guarded per CLAUDE.md setter convention.
func (h *DispatchHandler) SetSubmitter(s DispatchSubmitter) {
	if s != nil {
		h.submitter = s
	}
}

// RegisterDispatchMethods registers dispatch RPC methods on the server.
func (h *DispatchHandler) RegisterDispatchMethods(server *Server) {
	server.RegisterHandler("dispatch.submit", h.handleSubmit)
	server.RegisterHandler("dispatch.status", h.handleStatus)
	server.RegisterHandler("dispatch.results", h.handleResults)
}

// submitterOrErr returns the submitter or an error if unconfigured.
// Mirrors QueueHandler.reg() pattern.
func (h *DispatchHandler) submitterOrErr() (DispatchSubmitter, error) {
	if h.submitter == nil {
		return nil, errors.New("dispatch feature not enabled")
	}
	return h.submitter, nil
}

// handleSubmit handles dispatch.submit RPC calls.
func (h *DispatchHandler) handleSubmit(ctx context.Context, params json.RawMessage) (any, error) {
	sub, err := h.submitterOrErr()
	if err != nil {
		return nil, err
	}

	var req DispatchJobRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.TargetNode == "" {
		return nil, fmt.Errorf("target_node is required")
	}
	if req.AgentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	if req.TaskDescription == "" {
		return nil, fmt.Errorf("task_description is required")
	}

	ack, err := sub.Submit(ctx, req)
	if err != nil {
		h.logger.Error("dispatch.submit failed",
			"target_node", req.TargetNode,
			"agent_id", req.AgentID,
			"error", err)
		return nil, fmt.Errorf("dispatch submit failed: %w", err)
	}

	return ack, nil
}

// handleStatus handles dispatch.status RPC calls.
func (h *DispatchHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	sub, err := h.submitterOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	status, err := sub.Status(ctx, req.JobID)
	if err != nil {
		return nil, fmt.Errorf("dispatch status failed: %w", err)
	}
	return status, nil
}

// handleResults handles dispatch.results RPC calls.
func (h *DispatchHandler) handleResults(ctx context.Context, params json.RawMessage) (any, error) {
	sub, err := h.submitterOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.JobID == "" {
		return nil, fmt.Errorf("job_id is required")
	}

	results, err := sub.Results(ctx, req.JobID)
	if err != nil {
		return nil, fmt.Errorf("dispatch results failed: %w", err)
	}
	if results == nil {
		results = []DispatchResult{}
	}
	return results, nil
}

// DispatchTaskViaNode is the helper used by team.assign with "node:" prefix
// (α trigger surface). The daemon's AssignTask callback detects the prefix,
// splits the agentID into nodeID + agentID, and calls this function.
//
// nodePrefix is the portion after "node:" — i.e., the target node ID.
// agentID is the remaining agent identifier.
// task is the task description.
//
// Exported for daemon wiring (Phase 6).
func DispatchTaskViaNode(ctx context.Context, submitter DispatchSubmitter, nodeID, agentID, task string) (DispatchJobAck, error) {
	if submitter == nil {
		return DispatchJobAck{}, errors.New("dispatch feature not enabled")
	}
	if nodeID == "" {
		return DispatchJobAck{}, fmt.Errorf("node id is required")
	}
	if agentID == "" {
		return DispatchJobAck{}, fmt.Errorf("agent id is required")
	}
	if task == "" {
		return DispatchJobAck{}, fmt.Errorf("task is required")
	}

	req := DispatchJobRequest{
		TargetNode:      nodeID,
		AgentID:         agentID,
		TaskDescription: task,
	}
	return submitter.Submit(ctx, req)
}

// SplitNodePrefixedAgentID splits an agentID of the form "node:<nodeID>:<agentID>"
// into its constituent node ID and agent ID. Returns ok=false if the input
// does not have the "node:" prefix.
//
// Example: "node:abc-123:coder" → ("abc-123", "coder", true)
//
// If the prefix is present but no colon separates nodeID from agentID, the
// entire remainder is treated as the node ID and agentID is left empty.
func SplitNodePrefixedAgentID(agentID string) (nodeID, remainingAgentID string, ok bool) {
	const prefix = "node:"
	if !strings.HasPrefix(agentID, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(agentID, prefix)
	// Split on first colon: nodeID:agentID
	idx := strings.Index(rest, ":")
	if idx < 0 {
		// "node:<nodeID>" with no agent — treat rest as nodeID.
		return rest, "", true
	}
	return rest[:idx], rest[idx+1:], true
}
