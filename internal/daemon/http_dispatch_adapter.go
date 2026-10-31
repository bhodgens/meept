package daemon

import (
	"context"

	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/rpc"
)

// httpDispatchSubmitter adapts the rpc.DispatchSubmitter (cluster gRPC
// transport) to the comm/http DispatchSubmitter interface so the HTTP
// dispatch endpoints (/api/v1/dispatch/*) serve the same concrete
// submitter the RPC handler uses, instead of 503ing.
//
// Type mapping: the two interfaces are field-identical except Workspace,
// which is *rpc.WorkspaceRef on the rpc side and map[string]any on the
// HTTP side (JSON-native). The conversion is lossless in the
// rpc->http direction; the http->rpc direction reconstructs the struct
// from the map keys WorkspaceRef defines.
type httpDispatchSubmitter struct {
	rpc rpc.DispatchSubmitter
}

func (a *httpDispatchSubmitter) DispatchSubmit(ctx context.Context, req commhttp.DispatchJobRequest) (commhttp.DispatchJobAck, error) {
	ack, err := a.rpc.Submit(ctx, rpc.DispatchJobRequest{
		TargetNode:        req.TargetNode,
		AgentID:           req.AgentID,
		TaskDescription:   req.TaskDescription,
		RequiredResources: req.RequiredResources,
		Workspace:         workspaceFromMap(req.Workspace),
		Priority:          req.Priority,
	})
	if err != nil {
		return commhttp.DispatchJobAck{}, err
	}
	return commhttp.DispatchJobAck{JobID: ack.JobID, Accepted: ack.Accepted, Message: ack.Message}, nil
}

func (a *httpDispatchSubmitter) DispatchStatus(ctx context.Context, jobID string) (commhttp.DispatchJobResponse, error) {
	st, err := a.rpc.Status(ctx, jobID)
	if err != nil {
		return commhttp.DispatchJobResponse{}, err
	}
	return commhttp.DispatchJobResponse{
		JobID:     st.JobID,
		State:     st.State,
		StartedAt: st.StartedAt,
		UpdatedAt: st.UpdatedAt,
		Error:     st.Error,
	}, nil
}

func (a *httpDispatchSubmitter) DispatchResults(ctx context.Context, jobID string) ([]commhttp.DispatchResultEntry, error) {
	results, err := a.rpc.Results(ctx, jobID)
	if err != nil {
		return nil, err
	}
	out := make([]commhttp.DispatchResultEntry, 0, len(results))
	for _, r := range results {
		out = append(out, commhttp.DispatchResultEntry{
			JobID:       r.JobID,
			OutputRef:   r.OutputRef,
			Workspace:   workspaceToMap(r.Workspace),
			Error:       r.Error,
			CompletedAt: r.CompletedAt,
		})
	}
	return out, nil
}

func workspaceFromMap(m map[string]any) *rpc.WorkspaceRef {
	if m == nil {
		return nil
	}
	w := &rpc.WorkspaceRef{}
	if v, ok := m["repo_url"].(string); ok {
		w.RepoURL = v
	}
	if v, ok := m["commit_sha"].(string); ok {
		w.CommitSHA = v
	}
	if v, ok := m["diff_blob_hash"].(string); ok {
		w.DiffBlobHash = v
	}
	if v, ok := m["dirty"].(bool); ok {
		w.Dirty = v
	}
	return w
}

func workspaceToMap(w *rpc.WorkspaceRef) map[string]any {
	if w == nil {
		return nil
	}
	return map[string]any{
		"repo_url":       w.RepoURL,
		"commit_sha":     w.CommitSHA,
		"diff_blob_hash": w.DiffBlobHash,
		"dirty":          w.Dirty,
	}
}
