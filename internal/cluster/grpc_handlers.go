package cluster

// grpc_handlers.go — gRPC service handler implementations for the four
// cluster resource model services.
//
// Each handler struct holds a pointer to the GRPCTransport so it can
// resolve dependencies dynamically (providers may be set after Start).
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §4.3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/caimlas/meept/pkg/models"
)

// ErrPeerFetchNotImplemented indicates WorkspaceService.GitFetch is not yet
// wired (spec §6: only from registry-listed peers).
var ErrPeerFetchNotImplemented = errors.New("peer git fetch not implemented")

// ErrResourceProviderNotSet is returned when a ResourceService RPC arrives
// before SetResourceManager has wired the provider.
var ErrResourceProviderNotSet = errors.New("resource provider not set")

// ErrWorkspaceProviderNotSet is returned when a WorkspaceService RPC arrives
// before SetWorkspaceManager has wired the provider.
var ErrWorkspaceProviderNotSet = errors.New("workspace provider not set")

// ErrDispatchExecutorNotSet is returned when a DispatchService RPC arrives
// before SetExecutorBridge has wired the executor.
var ErrDispatchExecutorNotSet = errors.New("dispatch executor not set")

// ErrEventPublisherNotSet is returned when an EventService RPC arrives
// before SetEventPublisher has wired the publisher.
var ErrEventPublisherNotSet = errors.New("event publisher not set")

// =====================================================================
// EventService handler
// =====================================================================

// eventServiceHandler implements EventService.Publish and EventService.Broadcast.
// It resolves the EventPublisher from the transport at call time so providers
// can be wired after the server starts.
type eventServiceHandler struct {
	transport *GRPCTransport
	logger    *slog.Logger
}

func (h *eventServiceHandler) getPublisher() EventPublisher {
	h.transport.mu.RLock()
	defer h.transport.mu.RUnlock()
	return h.transport.eventPublisher
}

func (h *eventServiceHandler) publish(ctx context.Context, event *models.ClusterEvent) (Ack, error) {
	pub := h.getPublisher()
	if pub == nil {
		h.logger.Warn("grpc event_service: publisher not set")
		return Ack{Accepted: false, Message: "event publisher not available"}, nil
	}

	// Re-publish through the local gossip engine so the event reaches all peers.
	// The incoming event already has a payload; we propagate it as-is.
	err := pub.PublishClusterEvent(event.EventType, event.Payload)
	if err != nil {
		h.logger.Warn("grpc event_service: publish failed",
			"event_id", event.EventID,
			"event_type", event.EventType,
			"error", err,
		)
		return Ack{Accepted: false, Message: err.Error()}, nil
	}

	h.logger.Debug("grpc event_service: published",
		"event_id", event.EventID,
		"event_type", event.EventType,
	)
	return Ack{Accepted: true}, nil
}

// broadcast handles a bidirectional stream: read events from the peer,
// publish each locally, send back an Ack per event.
func (h *eventServiceHandler) broadcast(stream grpcStream) error {
	pub := h.getPublisher()
	if pub == nil {
		return ErrEventPublisherNotSet
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		event, ok := msg.(*models.ClusterEvent)
		if !ok {
			continue
		}
		pubErr := pub.PublishClusterEvent(event.EventType, event.Payload)
		ack := Ack{Accepted: pubErr == nil}
		if pubErr != nil {
			ack.Message = pubErr.Error()
		}
		if sendErr := stream.Send(&ack); sendErr != nil {
			return sendErr
		}
	}
}

// =====================================================================
// ResourceService handler
// =====================================================================

// resourceServiceHandler implements ResourceService.Has/Fetch/Stat.
type resourceServiceHandler struct {
	transport *GRPCTransport
	logger    *slog.Logger
}

func (h *resourceServiceHandler) getProvider() ResourceProvider {
	h.transport.mu.RLock()
	defer h.transport.mu.RUnlock()
	return h.transport.resourceProvider
}

func (h *resourceServiceHandler) has(ctx context.Context, req HasRequest) (HasResponse, error) {
	p := h.getProvider()
	if p == nil {
		return HasResponse{}, ErrResourceProviderNotSet
	}
	return HasResponse{Has: p.Has(req.Hash)}, nil
}

// fetch streams blob content in fetchChunkSize chunks, starting at req.Offset.
// Respects client context cancellation. HTTP/2 flow control is handled by grpc.
func (h *resourceServiceHandler) fetch(req FetchRequest, stream grpcServerStream) error {
	p := h.getProvider()
	if p == nil {
		return ErrResourceProviderNotSet
	}

	path, err := p.GetPath(req.Hash)
	if err != nil {
		return fmt.Errorf("resource fetch: get path for %s: %w", req.Hash, err)
	}

	// Open file (I/O outside any lock).
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("resource fetch: open %s: %w", path, err)
	}
	defer file.Close()

	// Seek to offset for resumable transfers.
	if req.Offset > 0 {
		if _, err := file.Seek(req.Offset, io.SeekStart); err != nil {
			return fmt.Errorf("resource fetch: seek to %d: %w", req.Offset, err)
		}
	}

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("resource fetch: stat: %w", err)
	}
	totalSize := stat.Size()

	buf := make([]byte, fetchChunkSize)
	offset := req.Offset

	for {
		// Check context cancellation (client disconnect / cancellation).
		select {
		case <-stream.Context().Done():
			h.logger.Debug("grpc resource_service: fetch cancelled by client",
				"hash", req.Hash,
				"offset", offset,
			)
			return stream.Context().Err()
		default:
		}

		n, readErr := file.Read(buf)
		if n > 0 {
			chunk := FetchChunk{
				Data:      buf[:n],
				Offset:    offset,
				TotalSize: totalSize,
				Hash:      req.Hash,
			}
			if sendErr := stream.Send(&chunk); sendErr != nil {
				return sendErr
			}
			offset += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return fmt.Errorf("resource fetch: read: %w", readErr)
		}
	}

	h.logger.Debug("grpc resource_service: fetch complete",
		"hash", req.Hash,
		"bytes_sent", offset-req.Offset,
	)
	return nil
}

func (h *resourceServiceHandler) stat(ctx context.Context, req StatRequest) (StatResponse, error) {
	p := h.getProvider()
	if p == nil {
		return StatResponse{}, ErrResourceProviderNotSet
	}

	size, addedAt, source, pinned, refcount, err := p.Stat(req.Hash)
	if err != nil {
		return StatResponse{}, fmt.Errorf("resource stat: %w", err)
	}

	return StatResponse{
		Size:     size,
		AddedAt:  addedAt.UnixNano(),
		Source:   source,
		Pinned:   pinned,
		Refcount: refcount,
	}, nil
}

// =====================================================================
// WorkspaceService handler
// =====================================================================

// workspaceServiceHandler implements WorkspaceService.Prepare and GitFetch.
type workspaceServiceHandler struct {
	transport *GRPCTransport
	logger    *slog.Logger
}

func (h *workspaceServiceHandler) getProvider() WorkspaceProvider {
	h.transport.mu.RLock()
	defer h.transport.mu.RUnlock()
	return h.transport.workspaceProvider
}

func (h *workspaceServiceHandler) prepare(ctx context.Context, ref WorkspaceRef) (WorkspaceReady, error) {
	p := h.getProvider()
	if p == nil {
		h.logger.Warn("grpc workspace_service: provider not set")
		return WorkspaceReady{Error: "workspace provider not available"}, nil
	}

	path, err := p.Ensure(ctx, ref)
	if err != nil {
		h.logger.Warn("grpc workspace_service: prepare failed",
			"repo_url", ref.RepoURL,
			"commit_sha", ref.CommitSHA,
			"error", err,
		)
		return WorkspaceReady{Error: err.Error()}, nil
	}

	h.logger.Debug("grpc workspace_service: prepared",
		"repo_url", ref.RepoURL,
		"commit_sha", ref.CommitSHA,
		"path", path,
	)
	return WorkspaceReady{WorktreePath: path, Ready: true}, nil
}

// gitFetch is a stub: spec §6 says "only from registry-listed peers".
// Returns ErrPeerFetchNotImplemented until peer-verification wiring lands.
func (h *workspaceServiceHandler) gitFetch(stream grpcStream) error {
	return ErrPeerFetchNotImplemented
}

// =====================================================================
// DispatchService handler
// =====================================================================

// dispatchServiceHandler implements DispatchService.Submit/Status/Results.
type dispatchServiceHandler struct {
	transport *GRPCTransport
	logger    *slog.Logger
}

func (h *dispatchServiceHandler) getExecutor() DispatchExecutor {
	h.transport.mu.RLock()
	defer h.transport.mu.RUnlock()
	return h.transport.dispatchExecutor
}

func (h *dispatchServiceHandler) submit(ctx context.Context, job DispatchJob) (DispatchJobAck, error) {
	e := h.getExecutor()
	if e == nil {
		h.logger.Warn("grpc dispatch_service: executor not set")
		return DispatchJobAck{JobID: job.JobID, Accepted: false, Message: "dispatch executor not available"}, nil
	}

	ack, err := e.SubmitJob(ctx, job)
	if err != nil {
		h.logger.Warn("grpc dispatch_service: submit failed",
			"job_id", job.JobID,
			"origin_node", job.OriginNode,
			"error", err,
		)
		return DispatchJobAck{JobID: job.JobID, Accepted: false, Message: err.Error()}, nil
	}

	h.logger.Debug("grpc dispatch_service: submitted",
		"job_id", job.JobID,
		"accepted", ack.Accepted,
	)
	return ack, nil
}

func (h *dispatchServiceHandler) status(ctx context.Context, jobID JobID) (JobStatus, error) {
	e := h.getExecutor()
	if e == nil {
		return JobStatus{}, ErrDispatchExecutorNotSet
	}

	status, err := e.JobStatus(ctx, jobID.ID)
	if err != nil {
		return JobStatus{}, fmt.Errorf("dispatch status: %w", err)
	}
	return status, nil
}

// results streams DispatchResult entries for a completed job.
func (h *dispatchServiceHandler) results(jobID JobID, stream grpcServerStream) error {
	e := h.getExecutor()
	if e == nil {
		return ErrDispatchExecutorNotSet
	}

	results, err := e.JobResults(stream.Context(), jobID.ID)
	if err != nil {
		return fmt.Errorf("dispatch results: %w", err)
	}

	for i := range results {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		default:
		}
		if sendErr := stream.Send(&results[i]); sendErr != nil {
			return sendErr
		}
	}

	return nil
}

// =====================================================================
// gRPC stream adapter interfaces
// =====================================================================

// grpcServerStream is the minimal interface the streaming handlers need from
// a grpc.ServerStream. The real grpc.ServerStream satisfies this. Using an
// interface avoids importing grpc in the handler file and simplifies testing.
type grpcServerStream interface {
	Context() context.Context
	Send(msg any) error
}

// grpcStream is the minimal interface for bidirectional streams.
type grpcStream interface {
	Recv() (any, error)
	Send(msg any) error
}
