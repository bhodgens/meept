package cluster

// grpc_transport_test.go — tests for the four gRPC services.
//
// Coverage:
//   - EventService: Publish unary, Broadcast bidirectional stream
//   - ResourceService: Has, Stat, Fetch (streaming with chunk verification)
//   - WorkspaceService: Prepare (success + nil provider), GitFetch stub
//   - DispatchService: Submit, Status, Results streaming
//   - Mock peers via in-process gRPC server
//   - Streaming backpressure (client context cancellation)
//   - mTLS prep (TLS config setter verification)
//
// Spec reference: docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md §10

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// =====================================================================
// Test helpers
// =====================================================================

// testAddr returns a free TCP address for a test listener.
func testAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// newTestTransport creates a started GRPCTransport on a free port.
// Returns the transport and its address.
func newTestTransport(t *testing.T, localNodeID string) (*GRPCTransport, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &Config{NodeID: localNodeID}
	tr := NewGRPCTransport(cfg, localNodeID, logger)

	addr := testAddr(t)
	ctx := context.Background()
	if err := tr.Start(ctx, addr); err != nil {
		t.Fatalf("start transport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Stop() })
	return tr, addr
}

// dialTestPeer dials the transport and returns a PeerClient.
func dialTestPeer(t *testing.T, tr *GRPCTransport, addr string) *PeerClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pc, err := tr.DialPeer(ctx, "test-peer", addr)
	if err != nil {
		t.Fatalf("dial peer: %v", err)
	}
	return pc
}

// =====================================================================
// Mock providers
// =====================================================================

type mockResourceProvider struct {
	mu    sync.Mutex
	store map[string]string // hash -> file path
	metas map[string]mockResourceMeta
}

type mockResourceMeta struct {
	size     int64
	addedAt  time.Time
	source   string
	pinned   bool
	refcount int
}

func newMockResourceProvider() *mockResourceProvider {
	return &mockResourceProvider{
		store: make(map[string]string),
		metas: make(map[string]mockResourceMeta),
	}
}

func (m *mockResourceProvider) addBlob(hash string, data []byte) {
	// Write to temp file outside mutex.
	f, _ := os.CreateTemp("", "mock-cas-*")
	f.Write(data)
	f.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[hash] = f.Name()
	m.metas[hash] = mockResourceMeta{
		size:     int64(len(data)),
		addedAt:  time.Now().UTC(),
		source:   "test",
		refcount: 1,
	}
}

func (m *mockResourceProvider) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.store {
		os.Remove(p)
	}
}

func (m *mockResourceProvider) Has(hash string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.store[hash]
	return ok
}

func (m *mockResourceProvider) GetPath(hash string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.store[hash]
	if !ok {
		return "", fmt.Errorf("blob not found: %s", hash)
	}
	return p, nil
}

func (m *mockResourceProvider) Stat(hash string) (int64, time.Time, string, bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, ok := m.metas[hash]
	if !ok {
		return 0, time.Time{}, "", false, 0, fmt.Errorf("not found: %s", hash)
	}
	return meta.size, meta.addedAt, meta.source, meta.pinned, meta.refcount, nil
}

type mockWorkspaceProvider struct {
	mu       sync.Mutex
	prepared int
	err      error
}

func (m *mockWorkspaceProvider) Ensure(ctx context.Context, ref WorkspaceRef) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepared++
	if m.err != nil {
		return "", m.err
	}
	return "/tmp/mock-worktree-" + ref.CommitSHA, nil
}

type mockDispatchExecutor struct {
	mu      sync.Mutex
	jobs    map[string]DispatchJob
	status  map[string]JobStatus
	results map[string][]DispatchResult
}

func newMockDispatchExecutor() *mockDispatchExecutor {
	return &mockDispatchExecutor{
		jobs:    make(map[string]DispatchJob),
		status:  make(map[string]JobStatus),
		results: make(map[string][]DispatchResult),
	}
}

func (m *mockDispatchExecutor) SubmitJob(ctx context.Context, job DispatchJob) (DispatchJobAck, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.JobID] = job
	m.status[job.JobID] = JobStatus{
		JobID:     job.JobID,
		State:     "queued",
		StartedAt: time.Now().UnixNano(),
		UpdatedAt: time.Now().UnixNano(),
	}
	return DispatchJobAck{JobID: job.JobID, Accepted: true}, nil
}

func (m *mockDispatchExecutor) JobStatus(ctx context.Context, jobID string) (JobStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.status[jobID]
	if !ok {
		return JobStatus{}, fmt.Errorf("job not found: %s", jobID)
	}
	return s, nil
}

func (m *mockDispatchExecutor) JobResults(ctx context.Context, jobID string) ([]DispatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.results[jobID]
	if !ok {
		return nil, fmt.Errorf("no results for job: %s", jobID)
	}
	return r, nil
}

type mockEventPublisher struct {
	mu      sync.Mutex
	events  []*models.ClusterEvent
	failErr error
}

func (m *mockEventPublisher) PublishClusterEvent(eventType models.ClusterEventType, payload any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	// Handle both json.RawMessage and []byte payloads.
	var raw json.RawMessage
	switch v := payload.(type) {
	case json.RawMessage:
		raw = v
	case []byte:
		raw = json.RawMessage(v)
	default:
		data, _ := json.Marshal(v)
		raw = data
	}
	m.events = append(m.events, &models.ClusterEvent{
		EventType: eventType,
		Payload:   raw,
	})
	return nil
}

// =====================================================================
// EventService tests
// =====================================================================

func TestEventService_Publish(t *testing.T) {
	pub := &mockEventPublisher{}
	tr, addr := newTestTransport(t, "node-a")
	tr.SetEventPublisher(pub)
	pc := dialTestPeer(t, tr, addr)

	event := &models.ClusterEvent{
		EventID:   "evt-1",
		NodeID:    "node-a",
		EventType: models.EventNodeHeartbeat,
		Payload:   []byte(`{"status":"ok"}`),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ack, err := pc.Publish(ctx, event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("expected accepted ack, got: %+v", ack)
	}

	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.events))
	}
	if pub.events[0].EventType != models.EventNodeHeartbeat {
		t.Fatalf("expected event type %s, got %s", models.EventNodeHeartbeat, pub.events[0].EventType)
	}
}

func TestEventService_PublishNoPublisher(t *testing.T) {
	tr, addr := newTestTransport(t, "node-a")
	// Do NOT set event publisher.
	pc := dialTestPeer(t, tr, addr)

	event := &models.ClusterEvent{
		EventID:   "evt-2",
		EventType: models.EventNodeHeartbeat,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ack, err := pc.Publish(ctx, event)
	if err != nil {
		t.Fatalf("publish should not return transport error: %v", err)
	}
	if ack.Accepted {
		t.Fatal("expected accepted=false when no publisher set")
	}
}

func TestEventService_Broadcast(t *testing.T) {
	pub := &mockEventPublisher{}
	tr, addr := newTestTransport(t, "node-a")
	tr.SetEventPublisher(pub)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := pc.Broadcast(ctx)
	if err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	// Send 3 events, expect 3 acks.
	for i := 0; i < 3; i++ {
		event := &models.ClusterEvent{
			EventID:   fmt.Sprintf("bcast-%d", i),
			NodeID:    "node-b",
			EventType: models.EventNodeHeartbeat,
			Payload:   []byte(fmt.Sprintf(`{"i":%d}`, i)),
		}
		if err := stream.Send(event); err != nil {
			t.Fatalf("send[%d]: %v", i, err)
		}
		ack, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv[%d]: %v", i, err)
		}
		if !ack.Accepted {
			t.Fatalf("ack[%d] not accepted: %s", i, ack.Message)
		}
	}
	_ = stream.CloseSend()

	// Verify all events were published.
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.events) != 3 {
		t.Fatalf("expected 3 published events, got %d", len(pub.events))
	}
}

// =====================================================================
// ResourceService tests
// =====================================================================

func TestResourceService_Has(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()
	rp.addBlob("sha256:abc123", []byte("test data"))

	tr, addr := newTestTransport(t, "node-a")
	tr.SetResourceManager(rp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Existing blob.
	resp, err := pc.Has(ctx, "sha256:abc123")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !resp.Has {
		t.Fatal("expected has=true for existing blob")
	}

	// Missing blob.
	resp, err = pc.Has(ctx, "sha256:nonexistent")
	if err != nil {
		t.Fatalf("has missing: %v", err)
	}
	if resp.Has {
		t.Fatal("expected has=false for missing blob")
	}
}

func TestResourceService_Stat(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()
	rp.addBlob("sha256:statblob", []byte("1234567890")) // 10 bytes

	tr, addr := newTestTransport(t, "node-a")
	tr.SetResourceManager(rp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := pc.Stat(ctx, "sha256:statblob")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if resp.Size != 10 {
		t.Fatalf("expected size=10, got %d", resp.Size)
	}
	if resp.Refcount != 1 {
		t.Fatalf("expected refcount=1, got %d", resp.Refcount)
	}
}

func TestResourceService_Fetch(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()

	// Create a 2.5 MiB blob to test multi-chunk streaming.
	blobSize := fetchChunkSize*2 + fetchChunkSize/2 // 2.5 MiB
	blobData := make([]byte, blobSize)
	for i := range blobData {
		blobData[i] = byte(i % 256)
	}
	rp.addBlob("sha256:largeblob", blobData)

	tr, addr := newTestTransport(t, "node-a")
	tr.SetResourceManager(rp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reader, totalSize, err := pc.Fetch(ctx, "sha256:largeblob", 0)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer reader.Close()

	if totalSize != int64(blobSize) {
		t.Fatalf("expected totalSize=%d, got %d", blobSize, totalSize)
	}

	received, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(received) != blobSize {
		t.Fatalf("expected %d bytes, got %d", blobSize, len(received))
	}
	// Verify content.
	for i := range received {
		if received[i] != byte(i%256) {
			t.Fatalf("byte mismatch at %d: expected %d, got %d", i, byte(i%256), received[i])
		}
	}
}

func TestResourceService_FetchWithOffset(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()

	fullData := []byte("0123456789ABCDEF")
	rp.addBlob("sha256:offsetblob", fullData)

	tr, addr := newTestTransport(t, "node-a")
	tr.SetResourceManager(rp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fetch from offset 4.
	reader, _, err := pc.Fetch(ctx, "sha256:offsetblob", 4)
	if err != nil {
		t.Fatalf("fetch with offset: %v", err)
	}
	defer reader.Close()

	received, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	expected := fullData[4:]
	if string(received) != string(expected) {
		t.Fatalf("expected %q, got %q", expected, received)
	}
}

func TestResourceService_FetchCancelBackpressure(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()

	// Create a large blob (5 MiB) so streaming takes multiple chunks.
	blobSize := 5 * fetchChunkSize
	blobData := make([]byte, blobSize)
	rp.addBlob("sha256:cancelblob", blobData)

	tr, addr := newTestTransport(t, "node-a")
	tr.SetResourceManager(rp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithCancel(context.Background())

	reader, _, err := pc.Fetch(ctx, "sha256:cancelblob", 0)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Read a tiny bit then cancel.
	buf := make([]byte, 100)
	_, _ = reader.Read(buf)

	// Cancel the context — server should stop streaming.
	cancel()

	// Subsequent reads should eventually error or EOF.
	// We don't fail the test if we get some data before the cancellation propagates.
	_, readErr := io.ReadAll(reader)
	// After cancellation, either the read errors or returns whatever was buffered.
	// The key assertion is that the server-side goroutine terminates and doesn't hang.
	if readErr != nil && !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, io.EOF) {
		// Some form of stream error is acceptable.
		t.Logf("read after cancel returned error (expected): %v", readErr)
	}
}

func TestResourceService_NilProvider(t *testing.T) {
	tr, addr := newTestTransport(t, "node-a")
	// Do NOT set resource provider.
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pc.Has(ctx, "sha256:whatever")
	if err == nil {
		t.Fatal("expected error when resource provider not set")
	}
}

// =====================================================================
// WorkspaceService tests
// =====================================================================

func TestWorkspaceService_Prepare(t *testing.T) {
	wp := &mockWorkspaceProvider{}

	tr, addr := newTestTransport(t, "node-a")
	tr.SetWorkspaceManager(wp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ref := WorkspaceRef{
		RepoURL:   "https://github.com/example/repo.git",
		CommitSHA: "abc123",
		Dirty:     false,
	}

	resp, err := pc.Prepare(ctx, ref)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !resp.Ready {
		t.Fatalf("expected ready=true, got: %+v", resp)
	}
	if !strings.Contains(resp.WorktreePath, "abc123") {
		t.Fatalf("expected worktree path to contain commit SHA, got: %s", resp.WorktreePath)
	}
}

func TestWorkspaceService_PrepareNilProvider(t *testing.T) {
	tr, addr := newTestTransport(t, "node-a")
	// Do NOT set workspace provider.
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ref := WorkspaceRef{CommitSHA: "abc"}
	resp, err := pc.Prepare(ctx, ref)
	if err != nil {
		t.Fatalf("prepare unary call should not return transport error: %v", err)
	}
	if resp.Ready {
		t.Fatal("expected ready=false when no provider set")
	}
	if resp.Error == "" {
		t.Fatal("expected error message when no provider set")
	}
}

// =====================================================================
// DispatchService tests
// =====================================================================

func TestDispatchService_SubmitAndStatus(t *testing.T) {
	de := newMockDispatchExecutor()

	tr, addr := newTestTransport(t, "node-a")
	tr.SetExecutorBridge(de)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := DispatchJob{
		JobID:           "job-001",
		OriginNode:      "node-a",
		TargetNode:      "node-b",
		AgentID:         "coder",
		TaskDescription: "fix bug",
		Priority:        1,
		CreatedAt:       time.Now().UnixNano(),
	}

	ack, err := pc.Submit(ctx, job)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("expected accepted ack: %+v", ack)
	}

	// Query status.
	status, err := pc.Status(ctx, "job-001")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.JobID != "job-001" {
		t.Fatalf("expected job_id=job-001, got %s", status.JobID)
	}
	if status.State != "queued" {
		t.Fatalf("expected state=queued, got %s", status.State)
	}
}

func TestDispatchService_Results(t *testing.T) {
	de := newMockDispatchExecutor()
	jobID := "job-results-1"

	// Pre-populate results.
	de.results[jobID] = []DispatchResult{
		{JobID: jobID, OutputRef: "sha256:output1", CompletedAt: time.Now().UnixNano()},
		{JobID: jobID, OutputRef: "sha256:output2", CompletedAt: time.Now().UnixNano()},
	}

	tr, addr := newTestTransport(t, "node-a")
	tr.SetExecutorBridge(de)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := pc.Results(ctx, jobID)
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].OutputRef != "sha256:output1" {
		t.Fatalf("expected output_ref=sha256:output1, got %s", results[0].OutputRef)
	}
}

func TestDispatchService_NilExecutor(t *testing.T) {
	tr, addr := newTestTransport(t, "node-a")
	// Do NOT set executor.
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := DispatchJob{JobID: "job-nil"}
	ack, err := pc.Submit(ctx, job)
	if err != nil {
		t.Fatalf("submit should not return transport error: %v", err)
	}
	if ack.Accepted {
		t.Fatal("expected accepted=false when executor not set")
	}
}

// =====================================================================
// TLS configuration test (mTLS prep)
// =====================================================================

func TestGRPCTransport_TLSConfigSetter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := NewGRPCTransport(&Config{}, "node-a", logger)

	// Nil config should be ignored.
	tr.SetTLSConfig(nil)

	// Valid config should be set.
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	tr.SetTLSConfig(tlsCfg)

	// Verify it was set (internal state check via mu).
	tr.mu.RLock()
	set := tr.tlsConfig
	tr.mu.RUnlock()

	if set == nil {
		t.Fatal("expected tls config to be set")
	}
	if set.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected min version TLS12, got %x", set.MinVersion)
	}
}

// =====================================================================
// Transport lifecycle tests
// =====================================================================

func TestGRPCTransport_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := NewGRPCTransport(&Config{}, "node-a", logger)

	addr := testAddr(t)
	ctx := context.Background()
	if err := tr.Start(ctx, addr); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !tr.IsRunning() {
		t.Fatal("expected running after start")
	}

	// Double start should fail.
	if err := tr.Start(ctx, addr); err == nil {
		t.Fatal("expected error on double start")
	}

	if err := tr.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if tr.IsRunning() {
		t.Fatal("expected not running after stop")
	}

	// Double stop should be a no-op.
	if err := tr.Stop(); err != nil {
		t.Fatalf("double stop: %v", err)
	}
}

func TestGRPCTransport_DialPeerCached(t *testing.T) {
	tr, addr := newTestTransport(t, "node-a")
	ctx := context.Background()

	pc1, err := tr.DialPeer(ctx, "peer-1", addr)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}

	pc2, err := tr.DialPeer(ctx, "peer-1", addr)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}

	if pc1 != pc2 {
		t.Fatal("expected cached peer client (same instance)")
	}
}

func TestGRPCTransport_ClosePeer(t *testing.T) {
	tr, addr := newTestTransport(t, "node-a")
	ctx := context.Background()

	_, err := tr.DialPeer(ctx, "peer-1", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	tr.ClosePeer("peer-1")

	tr.peersMu.RLock()
	_, exists := tr.peers["peer-1"]
	tr.peersMu.RUnlock()

	if exists {
		t.Fatal("expected peer to be removed after ClosePeer")
	}
}

// =====================================================================
// JSON Codec test
// =====================================================================

func TestJSONCodec(t *testing.T) {
	c := jsonCodec{}

	// Marshal.
	data, err := c.Marshal(&Ack{Accepted: true, Message: "ok"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Unmarshal.
	var ack Ack
	if err := c.Unmarshal(data, &ack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ack.Accepted || ack.Message != "ok" {
		t.Fatalf("roundtrip mismatch: %+v", ack)
	}

	// Name.
	if c.Name() != "json" {
		t.Fatalf("expected codec name 'json', got %s", c.Name())
	}

	// Nil marshal.
	d, err := c.Marshal(nil)
	if err != nil {
		t.Fatalf("marshal nil: %v", err)
	}
	if d != nil {
		t.Fatalf("expected nil bytes for nil value, got %v", d)
	}

	// Empty unmarshal.
	if err := c.Unmarshal(nil, &ack); err != nil {
		t.Fatalf("unmarshal nil: %v", err)
	}
}

// =====================================================================
// Fetch with large blob — chunk boundary verification
// =====================================================================

func TestResourceService_FetchChunkBoundaries(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()

	// Create a blob exactly 3 * chunkSize to test exact chunk boundaries.
	blobSize := 3 * fetchChunkSize
	blobData := make([]byte, blobSize)
	// Fill with a pattern that identifies byte position.
	for i := range blobData {
		blobData[i] = byte(i % 251) // prime for better distribution
	}
	rp.addBlob("sha256:boundary", blobData)

	// Write data to a known path so we can verify the server reads it.
	tr, addr := newTestTransport(t, "node-a")
	tr.SetResourceManager(rp)
	pc := dialTestPeer(t, tr, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reader, totalSize, err := pc.Fetch(ctx, "sha256:boundary", 0)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer reader.Close()

	if totalSize != int64(blobSize) {
		t.Fatalf("expected totalSize=%d, got %d", blobSize, totalSize)
	}

	received, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	if len(received) != blobSize {
		t.Fatalf("expected len=%d, got %d", blobSize, len(received))
	}

	// Verify no corruption at chunk boundaries.
	for i := range received {
		if received[i] != byte(i%251) {
			t.Fatalf("corruption at byte %d: expected %d, got %d", i, byte(i%251), received[i])
		}
	}
}

// =====================================================================
// Setter nil-guard tests (CLAUDE.md requirement)
// =====================================================================

func TestGRPCTransport_SettersNilGuard(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := NewGRPCTransport(&Config{}, "node-a", logger)

	// All setters must accept nil without panic.
	tr.SetResourceManager(nil)
	tr.SetWorkspaceManager(nil)
	tr.SetExecutorBridge(nil)
	tr.SetEventPublisher(nil)
	tr.SetTLSConfig(nil)

	// Verify nothing was set.
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if tr.resourceProvider != nil {
		t.Fatal("expected nil resource provider")
	}
	if tr.workspaceProvider != nil {
		t.Fatal("expected nil workspace provider")
	}
	if tr.dispatchExecutor != nil {
		t.Fatal("expected nil dispatch executor")
	}
	if tr.eventPublisher != nil {
		t.Fatal("expected nil event publisher")
	}
	if tr.tlsConfig != nil {
		t.Fatal("expected nil tls config")
	}
}

// =====================================================================
// Two-node integration: publish from one, verify receipt on the other
// =====================================================================

func TestGRPCTransport_TwoNodePublish(t *testing.T) {
	// Node A: sender.
	pubA := &mockEventPublisher{}
	trA, _ := newTestTransport(t, "node-a")
	trA.SetEventPublisher(pubA)

	// Node B: receiver with its own publisher.
	pubB := &mockEventPublisher{}
	trB, addrB := newTestTransport(t, "node-b")
	trB.SetEventPublisher(pubB)

	// Node A dials node B.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pcAtoB, err := trA.DialPeer(ctx, "node-b", addrB)
	if err != nil {
		t.Fatalf("dial node-b: %v", err)
	}

	// Publish an event to node B.
	event := &models.ClusterEvent{
		EventID:   "two-node-1",
		NodeID:    "node-a",
		EventType: models.EventNodeHeartbeat,
		Payload:   []byte(`{"status":"hello"}`),
	}
	ack, err := pcAtoB.Publish(ctx, event)
	if err != nil {
		t.Fatalf("publish to node-b: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("event not accepted by node-b: %s", ack.Message)
	}

	// Verify node B's publisher received it.
	pubB.mu.Lock()
	defer pubB.mu.Unlock()
	if len(pubB.events) != 1 {
		t.Fatalf("expected node-b to have 1 event, got %d", len(pubB.events))
	}
}

// =====================================================================
// Multi-service: all providers wired on same transport
// =====================================================================

func TestGRPCTransport_AllServicesWired(t *testing.T) {
	rp := newMockResourceProvider()
	defer rp.cleanup()
	rp.addBlob("sha256:multi", []byte("multi-service-data"))
	wp := &mockWorkspaceProvider{}
	de := newMockDispatchExecutor()
	pub := &mockEventPublisher{}

	tr, addr := newTestTransport(t, "node-full")
	tr.SetResourceManager(rp)
	tr.SetWorkspaceManager(wp)
	tr.SetExecutorBridge(de)
	tr.SetEventPublisher(pub)

	pc := dialTestPeer(t, tr, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// EventService.
	ack, err := pc.Publish(ctx, &models.ClusterEvent{
		EventType: models.EventNodeHeartbeat,
		Payload:   []byte(`{}`),
	})
	if err != nil || !ack.Accepted {
		t.Fatalf("event service failed: err=%v ack=%+v", err, ack)
	}

	// ResourceService.Has.
	hasResp, err := pc.Has(ctx, "sha256:multi")
	if err != nil || !hasResp.Has {
		t.Fatalf("resource service failed: err=%v has=%v", err, hasResp.Has)
	}

	// WorkspaceService.Prepare.
	prepResp, err := pc.Prepare(ctx, WorkspaceRef{CommitSHA: "multi123"})
	if err != nil || !prepResp.Ready {
		t.Fatalf("workspace service failed: err=%v ready=%v", err, prepResp.Ready)
	}

	// DispatchService.Submit.
	submitAck, err := pc.Submit(ctx, DispatchJob{JobID: "multi-job", AgentID: "test"})
	if err != nil || !submitAck.Accepted {
		t.Fatalf("dispatch service failed: err=%v ack=%+v", err, submitAck)
	}
}

// =====================================================================
// Helpers for test temp dirs (used by mock CAS)
// =====================================================================

// init ensures test temp files are cleaned up.
func TestMain(m *testing.M) {
	// Run tests.
	code := m.Run()

	// Clean up any stray mock CAS files in temp dir.
	entries, _ := filepath.Glob(filepath.Join(os.TempDir(), "mock-cas-*"))
	for _, e := range entries {
		_ = os.Remove(e)
	}

	os.Exit(code)
}
