package cluster

// grpc_transport.go — gRPC server + client for the cluster resource model.
//
// This file implements all four services (EventService, ResourceService,
// WorkspaceService, DispatchService) using manual ServiceDesc registration
// with a JSON codec, avoiding protoc codegen (spec §4.3, implementation note
// in task prompt).
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §4.3, §6

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/models"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

// =====================================================================
// JSON Codec
// =====================================================================

// jsonCodec implements encoding.Codec for gRPC using encoding/json.
// This avoids needing protoc-generated marshaling code.
type jsonCodec struct{}

func (jsonCodec) Name() string { return "json" }

func (jsonCodec) Marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// Ensure the codec is registered once at init.
var jsonCodecOnce sync.Once

func registerJSONCodec() {
	jsonCodecOnce.Do(func() {
		encoding.RegisterCodec(jsonCodec{})
	})
}

// jsonCallOption makes gRPC use the json codec for calls.
func jsonCallOption() grpc.CallOption {
	return grpc.CallContentSubtype("json")
}

// =====================================================================
// GRPCTransport
// =====================================================================

// GRPCTransport is the gRPC server + client manager for cluster resource
// model services. It replaces GossipTransport for resource/workspace/dispatch
// RPCs while GossipEngine continues to handle event broadcast.
type GRPCTransport struct {
	cfg         *Config
	localNodeID string
	logger      *slog.Logger

	// Injected dependencies (nil-safe — handlers check before use).
	resourceProvider ResourceProvider
	workspaceProvider WorkspaceProvider
	dispatchExecutor  DispatchExecutor
	eventPublisher    EventPublisher

	// TLS configuration. When nil, server uses insecure (dev/test mode).
	tlsConfig *tls.Config

	// Server state.
	mu       sync.RWMutex
	running  bool
	server   *grpc.Server
	listener net.Listener
	stopCh   chan struct{}
	doneCh   chan struct{}

	// Peer connection cache: nodeID -> *PeerClient
	peers   map[string]*PeerClient
	peersMu sync.RWMutex

	// peerAddrs maps node IDs to network addresses for lazy dialing.
	peerAddrs *peerAddrMap
}

// NewGRPCTransport creates a new transport. The transport is not started;
// call Start to bind the listener.
func NewGRPCTransport(cfg *Config, localNodeID string, logger *slog.Logger) *GRPCTransport {
	if logger == nil {
		logger = slog.Default()
	}
	registerJSONCodec()
	return &GRPCTransport{
		cfg:         cfg,
		localNodeID: localNodeID,
		logger:      logger,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		peers:       make(map[string]*PeerClient),
		peerAddrs:   newPeerAddrMap(),
	}
}

// =====================================================================
// Setters (all nil-guarded per CLAUDE.md convention)
// =====================================================================

// SetResourceManager injects the CAS resource provider for ResourceService.
func (t *GRPCTransport) SetResourceManager(p ResourceProvider) {
	if p != nil {
		t.mu.Lock()
		t.resourceProvider = p
		t.mu.Unlock()
	}
}

// SetWorkspaceManager injects the workspace provider for WorkspaceService.
func (t *GRPCTransport) SetWorkspaceManager(p WorkspaceProvider) {
	if p != nil {
		t.mu.Lock()
		t.workspaceProvider = p
		t.mu.Unlock()
	}
}

// SetExecutorBridge injects the dispatch executor for DispatchService.
func (t *GRPCTransport) SetExecutorBridge(e DispatchExecutor) {
	if e != nil {
		t.mu.Lock()
		t.dispatchExecutor = e
		t.mu.Unlock()
	}
}

// SetEventPublisher injects the event publisher for EventService.
func (t *GRPCTransport) SetEventPublisher(p EventPublisher) {
	if p != nil {
		t.mu.Lock()
		t.eventPublisher = p
		t.mu.Unlock()
	}
}

// SetTLSConfig sets the TLS configuration for mTLS. When nil, insecure mode
// is used (dev/test only). Spec §6: mTLS over WireGuard.
func (t *GRPCTransport) SetTLSConfig(c *tls.Config) {
	if c != nil {
		t.mu.Lock()
		t.tlsConfig = c
		t.mu.Unlock()
	}
}

// =====================================================================
// Server lifecycle
// =====================================================================

// Start binds the listener and starts serving all four gRPC services.
// listenAddr is in "host:port" format (e.g., ":51822" or "10.200.0.1:51822").
func (t *GRPCTransport) Start(ctx context.Context, listenAddr string) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("grpc transport already running")
	}
	t.mu.Unlock()

	// Collect TLS config under lock (handlers resolve providers dynamically).
	t.mu.RLock()
	tlsCfg := t.tlsConfig
	t.mu.RUnlock()

	// Build handlers. They reference the transport so providers can be
	// set after Start without needing to restart the server.
	eventHandler := &eventServiceHandler{transport: t, logger: t.logger}
	resourceHandler := &resourceServiceHandler{transport: t, logger: t.logger}
	workspaceHandler := &workspaceServiceHandler{transport: t, logger: t.logger}
	dispatchHandler := &dispatchServiceHandler{transport: t, logger: t.logger}

	// Create listener (I/O outside lock).
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("grpc transport: listen on %s: %w", listenAddr, err)
	}

	// Build gRPC server with interceptors and optional TLS.
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(t.unaryInterceptor),
		grpc.StreamInterceptor(t.streamInterceptor),
	}
	if tlsCfg != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}
	server := grpc.NewServer(opts...)

	// Register all four services.
	registerEventService(server, eventHandler)
	registerResourceService(server, resourceHandler)
	registerWorkspaceService(server, workspaceHandler)
	registerDispatchService(server, dispatchHandler)

	t.mu.Lock()
	t.running = true
	t.server = server
	t.listener = listener
	t.mu.Unlock()

	go t.serve()

	t.logger.Info("grpc_transport: listening",
		"address", listenAddr,
		"node_id", t.localNodeID,
		"tls", tlsCfg != nil,
	)

	return nil
}

// serve runs the gRPC server until Stop is called.
func (t *GRPCTransport) serve() {
	defer close(t.doneCh)
	t.mu.RLock()
	server := t.server
	listener := t.listener
	t.mu.RUnlock()

	if server == nil || listener == nil {
		return
	}
	_ = server.Serve(listener) // blocks until Stop
}

// Stop gracefully shuts down the gRPC server and closes all peer connections.
func (t *GRPCTransport) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	server := t.server
	t.mu.Unlock()

	if server != nil {
		server.GracefulStop()
	}

	// Close all peer clients.
	t.peersMu.Lock()
	peers := t.peers
	t.peers = make(map[string]*PeerClient)
	t.peersMu.Unlock()
	for _, pc := range peers {
		_ = pc.Close()
	}

	t.logger.Info("grpc_transport: stopped")
	return nil
}

// IsRunning returns whether the transport is currently active.
func (t *GRPCTransport) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// =====================================================================
// Interceptors
// =====================================================================

func (t *GRPCTransport) unaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	duration := time.Since(start)
	if err != nil {
		t.logger.Debug("grpc unary: error",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
	} else {
		t.logger.Debug("grpc unary: ok",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
		)
	}
	return resp, err
}

func (t *GRPCTransport) streamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	start := time.Now()
	err := handler(srv, ss)
	duration := time.Since(start)
	if err != nil {
		t.logger.Debug("grpc stream: error",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
	} else {
		t.logger.Debug("grpc stream: ok",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
		)
	}
	return err
}

// =====================================================================
// Peer client management
// =====================================================================

// DialPeer establishes (or retrieves cached) a gRPC connection to a peer.
// The connection is reused for subsequent calls to the same peer. When addr
// is empty, the transport falls back to its peer address registry (populated
// via RegisterPeerAddr).
func (t *GRPCTransport) DialPeer(ctx context.Context, nodeID, addr string) (*PeerClient, error) {
	// Check cache.
	t.peersMu.RLock()
	if pc, ok := t.peers[nodeID]; ok {
		t.peersMu.RUnlock()
		return pc, nil
	}
	t.peersMu.RUnlock()

	// Fall back to registered address when addr is empty.
	if addr == "" && t.peerAddrs != nil {
		if registered, ok := t.peerAddrs.Get(nodeID); ok {
			addr = registered
		}
	}
	if addr == "" {
		return nil, fmt.Errorf("grpc transport: no address known for peer %s", nodeID)
	}

	// Collect TLS config under lock.
	t.mu.RLock()
	tlsCfg := t.tlsConfig
	t.mu.RUnlock()

	dialOpts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(jsonCallOption()),
	}
	if tlsCfg != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("grpc transport: dial peer %s at %s: %w", nodeID, addr, err)
	}

	pc := &PeerClient{
		nodeID: nodeID,
		addr:   addr,
		conn:   conn,
		logger: t.logger,
	}

	t.peersMu.Lock()
	t.peers[nodeID] = pc
	t.peersMu.Unlock()

	t.logger.Debug("grpc_transport: dialed peer",
		"node_id", nodeID,
		"addr", addr,
		"tls", tlsCfg != nil,
	)

	return pc, nil
}

// ClosePeer closes the cached connection to a specific peer.
func (t *GRPCTransport) ClosePeer(nodeID string) {
	t.peersMu.Lock()
	pc, ok := t.peers[nodeID]
	if ok {
		delete(t.peers, nodeID)
	}
	t.peersMu.Unlock()
	if ok {
		_ = pc.Close()
	}
}

// PeerList returns the node IDs of all known peers — both cached
// connections and registered-but-not-yet-dialed addresses. The slice is a
// snapshot; callers should not rely on it being stable.
func (t *GRPCTransport) PeerList() []string {
	seen := make(map[string]bool)

	// Cached connections.
	t.peersMu.RLock()
	for nodeID := range t.peers {
		seen[nodeID] = true
	}
	t.peersMu.RUnlock()

	// Registered addresses.
	if t.peerAddrs != nil {
		t.peerAddrs.mu.RLock()
		for nodeID := range t.peerAddrs.addrs {
			seen[nodeID] = true
		}
		t.peerAddrs.mu.RUnlock()
	}

	out := make([]string, 0, len(seen))
	for nodeID := range seen {
		out = append(out, nodeID)
	}
	return out
}

// peerAddrs stores known peer addresses (nodeID → "host:port") for
// dialing peers that haven't been explicitly dialed yet. Populated by
// RegisterPeerAddr (called from gossip membership or manual config).
type peerAddrMap struct {
	mu    sync.RWMutex
	addrs map[string]string
}

func newPeerAddrMap() *peerAddrMap {
	return &peerAddrMap{addrs: make(map[string]string)}
}

func (p *peerAddrMap) Set(nodeID, addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addrs[nodeID] = addr
}

func (p *peerAddrMap) Get(nodeID string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	addr, ok := p.addrs[nodeID]
	return addr, ok
}

// RegisterPeerAddr records the network address for a peer node so
// DialPeer can establish connections without an explicit addr parameter.
// This is typically called when gossip membership discovers a new node.
func (t *GRPCTransport) RegisterPeerAddr(nodeID, addr string) {
	t.peerAddrs.Set(nodeID, addr)
}

// =====================================================================
// PeerClient — thin wrapper over grpc.ClientConn for all four services
// =====================================================================

// PeerClient wraps a gRPC connection to a peer daemon and exposes typed
// methods for each service RPC.
type PeerClient struct {
	nodeID string
	addr   string
	conn   *grpc.ClientConn
	logger *slog.Logger
}

// Close releases the underlying gRPC connection.
func (pc *PeerClient) Close() error {
	if pc.conn != nil {
		return pc.conn.Close()
	}
	return nil
}

// ----- EventService -----

// Publish sends a single cluster event to the peer.
func (pc *PeerClient) Publish(ctx context.Context, event *models.ClusterEvent) (Ack, error) {
	var ack Ack
	err := pc.conn.Invoke(ctx, "/cluster.EventService/Publish", event, &ack, jsonCallOption())
	return ack, err
}

// Broadcast opens a bidirectional stream. The caller drives the stream
// via the returned BroadcastStream.
func (pc *PeerClient) Broadcast(ctx context.Context) (BroadcastStream, error) {
	stream, err := pc.conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "Broadcast",
		ServerStreams: true,
		ClientStreams: true,
	}, "/cluster.EventService/Broadcast", jsonCallOption())
	if err != nil {
		return nil, err
	}
	return &broadcastStreamImpl{stream: stream}, nil
}

// BroadcastStream is the bidirectional stream for EventService.Broadcast.
type BroadcastStream interface {
	Send(event *models.ClusterEvent) error
	Recv() (Ack, error)
	CloseSend() error
}

type broadcastStreamImpl struct {
	stream grpc.ClientStream
}

func (s *broadcastStreamImpl) Send(event *models.ClusterEvent) error {
	return s.stream.SendMsg(event)
}

func (s *broadcastStreamImpl) Recv() (Ack, error) {
	var ack Ack
	err := s.stream.RecvMsg(&ack)
	return ack, err
}

func (s *broadcastStreamImpl) CloseSend() error {
	return s.stream.CloseSend()
}

// ----- ResourceService -----

// Has checks if the peer has a blob in its CAS store.
func (pc *PeerClient) Has(ctx context.Context, hash string) (HasResponse, error) {
	var resp HasResponse
	err := pc.conn.Invoke(ctx, "/cluster.ResourceService/Has", &HasRequest{Hash: hash}, &resp, jsonCallOption())
	return resp, err
}

// Stat requests CAS metadata from the peer.
func (pc *PeerClient) Stat(ctx context.Context, hash string) (StatResponse, error) {
	var resp StatResponse
	err := pc.conn.Invoke(ctx, "/cluster.ResourceService/Stat", &StatRequest{Hash: hash}, &resp, jsonCallOption())
	return resp, err
}

// Fetch opens a streaming download of a blob from the peer's CAS store.
// Returns an io.ReadCloser for the blob content and the total size.
// The offset parameter enables resumable transfers (spec §4.3).
func (pc *PeerClient) Fetch(ctx context.Context, hash string, offset int64) (io.ReadCloser, int64, error) {
	stream, err := pc.conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "Fetch",
		ServerStreams: true,
	}, "/cluster.ResourceService/Fetch", jsonCallOption())
	if err != nil {
		return nil, 0, err
	}

	if err := stream.SendMsg(&FetchRequest{Hash: hash, Offset: offset}); err != nil {
		return nil, 0, err
	}
	if err := stream.CloseSend(); err != nil {
		return nil, 0, err
	}

	// Read the first chunk synchronously to extract totalSize.
	var firstChunk FetchChunk
	if err := stream.RecvMsg(&firstChunk); err != nil {
		if errors.Is(err, io.EOF) {
			// Empty blob — return a reader that yields nothing.
			return io.NopCloser(strings.NewReader("")), 0, nil
		}
		return nil, 0, err
	}
	totalSize := firstChunk.TotalSize

	// If this was the only chunk, return its data directly.
	secondChunkCheck := &FetchChunk{}
	secondErr := stream.RecvMsg(secondChunkCheck)

	if errors.Is(secondErr, io.EOF) {
		// Only one chunk total.
		return io.NopCloser(bytes.NewReader(firstChunk.Data)), totalSize, nil
	}

	// Multi-chunk: pipe first + second and stream the rest.
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		// Write first chunk data.
		if _, err := pw.Write(firstChunk.Data); err != nil {
			return
		}
		// Write second chunk (already received).
		if secondErr == nil {
			if _, err := pw.Write(secondChunkCheck.Data); err != nil {
				return
			}
		}
		// Stream remaining chunks.
		for {
			var chunk FetchChunk
			if err := stream.RecvMsg(&chunk); err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				_ = pw.CloseWithError(err)
				return
			}
			if _, err := pw.Write(chunk.Data); err != nil {
				return
			}
		}
	}()

	return pr, totalSize, nil
}

// ----- WorkspaceService -----

// Prepare asks the peer to materialize a workspace.
func (pc *PeerClient) Prepare(ctx context.Context, ref WorkspaceRef) (WorkspaceReady, error) {
	var resp WorkspaceReady
	err := pc.conn.Invoke(ctx, "/cluster.WorkspaceService/Prepare", &ref, &resp, jsonCallOption())
	return resp, err
}

// ----- DispatchService -----

// Submit sends a dispatch job to the peer.
func (pc *PeerClient) Submit(ctx context.Context, job DispatchJob) (DispatchJobAck, error) {
	var ack DispatchJobAck
	err := pc.conn.Invoke(ctx, "/cluster.DispatchService/Submit", &job, &ack, jsonCallOption())
	return ack, err
}

// Status queries the peer for a job's current state.
func (pc *PeerClient) Status(ctx context.Context, jobID string) (JobStatus, error) {
	var resp JobStatus
	err := pc.conn.Invoke(ctx, "/cluster.DispatchService/Status", &JobID{ID: jobID}, &resp, jsonCallOption())
	return resp, err
}

// Results streams completion results for a job from the peer.
func (pc *PeerClient) Results(ctx context.Context, jobID string) ([]DispatchResult, error) {
	stream, err := pc.conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "Results",
		ServerStreams: true,
	}, "/cluster.DispatchService/Results", jsonCallOption())
	if err != nil {
		return nil, err
	}

	if err := stream.SendMsg(&JobID{ID: jobID}); err != nil {
		return nil, err
	}
	if err := stream.CloseSend(); err != nil {
		return nil, err
	}

	var results []DispatchResult
	for {
		var result DispatchResult
		if err := stream.RecvMsg(&result); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// =====================================================================
// ServiceDesc registrations
// =====================================================================

// grpc requires HandlerType to be a pointer-to-interface so that
// reflect.TypeOf(ht).Elem() yields an interface type. We define minimal
// server interfaces for each service to satisfy this requirement.
// The concrete handler structs implement these interfaces.

// EventServiceServer is the interface for EventService handlers.
type EventServiceServer interface {
	publish(ctx context.Context, event *models.ClusterEvent) (Ack, error)
}

// ResourceServiceServer is the interface for ResourceService handlers.
type ResourceServiceServer interface {
	has(ctx context.Context, req HasRequest) (HasResponse, error)
	stat(ctx context.Context, req StatRequest) (StatResponse, error)
}

// WorkspaceServiceServer is the interface for WorkspaceService handlers.
type WorkspaceServiceServer interface {
	prepare(ctx context.Context, ref WorkspaceRef) (WorkspaceReady, error)
}

// DispatchServiceServer is the interface for DispatchService handlers.
type DispatchServiceServer interface {
	submit(ctx context.Context, job DispatchJob) (DispatchJobAck, error)
	status(ctx context.Context, jobID JobID) (JobStatus, error)
}

//----- helpers for method handler dispatch -----

// We use grpc.MethodDesc with handler functions that decode/encode using the
// registered json codec. The handler signature is:
//   func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error)

// makeUnaryHandler creates a grpc method handler that decodes the request,
// calls the service method, and returns the response.
func makeUnaryHandler[Req, Resp any](
	method func(ctx context.Context, req Req) (Resp, error),
) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		req := new(Req)
		if err := dec(req); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to decode request: %v", err)
		}
		if interceptor == nil {
			return method(ctx, *req)
		}
		info := &grpc.UnaryServerInfo{
			Server: srv,
		}
		handler := func(ctx context.Context, req any) (any, error) {
			return method(ctx, req.(Req))
		}
		return interceptor(ctx, *req, info, handler)
	}
}

// ----- EventService ServiceDesc -----

func registerEventService(server *grpc.Server, h *eventServiceHandler) {
	serviceDesc := grpc.ServiceDesc{
		ServiceName: "cluster.EventService",
		HandlerType: (*EventServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Publish",
				Handler:    makeUnaryHandler(h.publish),
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Broadcast",
				Handler:       eventBroadcastHandler(h),
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}
	server.RegisterService(&serviceDesc, h)
}

// eventBroadcastHandler adapts the bidirectional Broadcast RPC to grpc's
// stream handler signature.
func eventBroadcastHandler(h *eventServiceHandler) grpc.StreamHandler {
	return func(srv any, stream grpc.ServerStream) error {
		return h.broadcast(&eventBroadcastStreamAdapter{serverStream: stream})
	}
}

// eventBroadcastStreamAdapter wraps grpc.ServerStream to satisfy the
// grpcStream interface. Recv returns *models.ClusterEvent.
type eventBroadcastStreamAdapter struct {
	serverStream grpc.ServerStream
}

func (a *eventBroadcastStreamAdapter) Recv() (any, error) {
	var event models.ClusterEvent
	if err := a.serverStream.RecvMsg(&event); err != nil {
		return nil, err
	}
	return &event, nil
}

func (a *eventBroadcastStreamAdapter) Send(msg any) error {
	return a.serverStream.SendMsg(msg)
}

// ----- ResourceService ServiceDesc -----

func registerResourceService(server *grpc.Server, h *resourceServiceHandler) {
	serviceDesc := grpc.ServiceDesc{
		ServiceName: "cluster.ResourceService",
		HandlerType: (*ResourceServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Has",
				Handler:    makeUnaryHandler(h.has),
			},
			{
				MethodName: "Stat",
				Handler:    makeUnaryHandler(h.stat),
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Fetch",
				Handler:       resourceFetchHandler(h),
				ServerStreams: true,
			},
		},
	}
	server.RegisterService(&serviceDesc, h)
}

// resourceFetchHandler adapts the server-streaming Fetch RPC.
func resourceFetchHandler(h *resourceServiceHandler) grpc.StreamHandler {
	return func(srv any, stream grpc.ServerStream) error {
		var req FetchRequest
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		return h.fetch(req, &resourceServerStreamAdapter{serverStream: stream})
	}
}

// resourceServerStreamAdapter wraps grpc.ServerStream for Fetch streaming.
type resourceServerStreamAdapter struct {
	serverStream grpc.ServerStream
}

func (a *resourceServerStreamAdapter) Context() context.Context {
	return a.serverStream.Context()
}

func (a *resourceServerStreamAdapter) Send(msg any) error {
	return a.serverStream.SendMsg(msg)
}

// ----- WorkspaceService ServiceDesc -----

func registerWorkspaceService(server *grpc.Server, h *workspaceServiceHandler) {
	serviceDesc := grpc.ServiceDesc{
		ServiceName: "cluster.WorkspaceService",
		HandlerType: (*WorkspaceServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Prepare",
				Handler:    makeUnaryHandler(h.prepare),
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "GitFetch",
				Handler:       workspaceGitFetchHandler(h),
				ServerStreams: true,
				ClientStreams: true,
			},
		},
	}
	server.RegisterService(&serviceDesc, h)
}

// workspaceGitFetchHandler adapts the bidirectional GitFetch RPC.
func workspaceGitFetchHandler(h *workspaceServiceHandler) grpc.StreamHandler {
	return func(srv any, stream grpc.ServerStream) error {
		return h.gitFetch(&workspaceStreamAdapter{serverStream: stream})
	}
}

// workspaceStreamAdapter wraps grpc.ServerStream for GitFetch.
type workspaceStreamAdapter struct {
	serverStream grpc.ServerStream
}

func (a *workspaceStreamAdapter) Recv() (any, error) {
	var req GitObjectRequest
	if err := a.serverStream.RecvMsg(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (a *workspaceStreamAdapter) Send(msg any) error {
	return a.serverStream.SendMsg(msg)
}

// ----- DispatchService ServiceDesc -----

func registerDispatchService(server *grpc.Server, h *dispatchServiceHandler) {
	serviceDesc := grpc.ServiceDesc{
		ServiceName: "cluster.DispatchService",
		HandlerType: (*DispatchServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Submit",
				Handler:    makeUnaryHandler(h.submit),
			},
			{
				MethodName: "Status",
				Handler:    makeUnaryHandler(h.status),
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "Results",
				Handler:       dispatchResultsHandler(h),
				ServerStreams: true,
			},
		},
	}
	server.RegisterService(&serviceDesc, h)
}

// dispatchResultsHandler adapts the server-streaming Results RPC.
func dispatchResultsHandler(h *dispatchServiceHandler) grpc.StreamHandler {
	return func(srv any, stream grpc.ServerStream) error {
		var jobID JobID
		if err := stream.RecvMsg(&jobID); err != nil {
			return err
		}
		return h.results(jobID, &dispatchServerStreamAdapter{serverStream: stream})
	}
}

// dispatchServerStreamAdapter wraps grpc.ServerStream for Results streaming.
type dispatchServerStreamAdapter struct {
	serverStream grpc.ServerStream
}

func (a *dispatchServerStreamAdapter) Context() context.Context {
	return a.serverStream.Context()
}

func (a *dispatchServerStreamAdapter) Send(msg any) error {
	return a.serverStream.SendMsg(msg)
}
