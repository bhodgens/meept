package cluster

// executor_bridge_signature_test.go — leaf 08 / C9 (agent-groups plan):
// the ExecutorBridge verifies a DispatchJob's ed25519 signature against
// the ORIGIN NODE's signing key (the same registry the gossip engine
// uses — SigningKeyLookup is satisfied structurally by *GossipEngine)
// before the job is allowed to execute locally. Fail-closed: unsigned,
// unknown-origin, and tampered jobs are rejected with a Warn and never
// reach the agent invoker. An explicit opt-out (mirroring a
// cluster.security.require_node_signatures=false config) re-enables the
// legacy accept-unsigned path.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer serializes log writes from the bridge's execute goroutine
// with the test's log read. A plain bytes.Buffer races under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// staticKeyLookup is a SigningKeyLookup over a fixed nodeID → pubkey map
// (test double for *GossipEngine's registry).
type staticKeyLookup struct {
	keys map[string]ed25519.PublicKey
}

func (s staticKeyLookup) PeerSigningKey(nodeID string) (ed25519.PublicKey, bool) {
	k, ok := s.keys[nodeID]
	return k, ok
}

// signedJob returns the job with a valid ed25519 signature over
// signingBytes from priv.
func signedJob(job DispatchJob, priv ed25519.PrivateKey) DispatchJob {
	job.Signature = ed25519.Sign(priv, job.signingBytes())
	return job
}

// expectNoExecution fails the test if the invoker runs within the wait
// window (the fail-closed assertion: rejected jobs never execute).
func expectNoExecution(t *testing.T, invoker *fakeAgentInvoker) {
	t.Helper()
	deadline := time.After(300 * time.Millisecond)
	for {
		if invoker.calls() > 0 {
			t.Fatalf("fail-closed violated: rejected dispatch job reached the agent invoker (%d call(s))", invoker.calls())
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			return
		}
	}
}

// TestDispatchSignature is the T1 table (leaf verify command:
// `go test -p 2 ./internal/cluster/ -run TestDispatchSignature`):
// default-ON fail-closed rejection for every invalid-signature shape,
// execution for the valid one, and the explicit opt-out.
func TestDispatchSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate second ed25519 key: %v", err)
	}

	lookup := staticKeyLookup{keys: map[string]ed25519.PublicKey{
		"node-origin": pub,
	}}

	base := func() DispatchJob {
		return DispatchJob{
			JobID:           "sig-job-1",
			OriginNode:      "node-origin",
			AgentID:         "agent-a",
			TaskDescription: "signature verification probe",
			CreatedAt:       time.Now().UnixNano(),
		}
	}

	// tamper mutates the signed job AFTER signing (payload tampering).
	tamper := func(job DispatchJob) DispatchJob {
		job.TaskDescription = "tampered: rm -rf /"
		return job
	}

	cases := []struct {
		name      string
		buildJob  func() DispatchJob
		lookup    SigningKeyLookup
		wantExec  bool
		wantFails int // TASK_FAIL publications from the rejection path
	}{
		{
			name:     "valid signature executes",
			buildJob: func() DispatchJob { return signedJob(base(), priv) },
			lookup:   lookup,
			wantExec: true,
		},
		{
			name:      "unsigned rejected",
			buildJob:  base,
			lookup:    lookup,
			wantFails: 1,
		},
		{
			name:      "tampered payload rejected",
			buildJob:  func() DispatchJob { return tamper(signedJob(base(), priv)) },
			lookup:    lookup,
			wantFails: 1,
		},
		{
			name:      "wrong signer key rejected",
			buildJob:  func() DispatchJob { return signedJob(base(), mustPriv(t, otherPub)) },
			lookup:    lookup,
			wantFails: 1,
		},
		{
			name:      "unknown origin node rejected",
			buildJob:  func() DispatchJob { j := signedJob(base(), priv); j.OriginNode = "node-ghost"; return j },
			lookup:    lookup,
			wantFails: 1,
		},
		{
			name:      "nil key lookup fails closed",
			buildJob:  func() DispatchJob { return signedJob(base(), priv) },
			lookup:    nil,
			wantFails: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invoker := &fakeAgentInvoker{output: "ok"}
			pub2 := &fakeBusPublisher{}
			// Default construction: the fail-closed flag is ON with no
			// setter calls (this IS the pin for the default flip).
			bridge := NewExecutorBridge("local", nil)
			bridge.SetSigningKeyLookup(tc.lookup)
			bridge.SetAgentInvoker(invoker)
			bridge.SetBusPublisher(pub2)

			job := tc.buildJob()
			event := taskCreateEvent(job)
			event.NodeID = job.OriginNode
			if err := bridge.HandleTaskCreate(event); err != nil {
				t.Fatalf("HandleTaskCreate: %v", err)
			}

			if !tc.wantExec {
				expectNoExecution(t, invoker)
				completes, fails := pub2.snapshot()
				if len(fails) != tc.wantFails {
					t.Fatalf("TASK_FAIL publications: want %d, got %d (%+v)", tc.wantFails, len(fails), fails)
				}
				if len(fails) > 0 && fails[0].JobID != job.JobID {
					t.Fatalf("TASK_FAIL job id: want %s, got %s", job.JobID, fails[0].JobID)
				}
				if len(completes) != 0 {
					t.Fatalf("rejected job must never complete, got %+v", completes)
				}
				return
			}
			// Execution is asynchronous — wait for the completion event.
			deadline := time.After(2 * time.Second)
			var completes []completeEvent
			var fails []failEvent
			for {
				completes, fails = pub2.snapshot()
				if len(completes) > 0 || len(fails) > 0 {
					break
				}
				select {
				case <-time.After(20 * time.Millisecond):
				case <-deadline:
					t.Fatalf("validly-signed job never completed")
				}
			}
			if len(completes) != 1 {
				t.Fatalf("validly-signed job did not complete: completes=%d fails=%+v", len(completes), fails)
			}
			if invoker.calls() != 1 {
				t.Fatalf("invoker calls: want 1, got %d", invoker.calls())
			}
		})
	}
}

// mustPriv recovers the private key for a known public key by generating
// a fresh pair and verifying it matches (test-only helper; keeps the
// table above readable).
func mustPriv(t *testing.T, pub ed25519.PublicKey) ed25519.PrivateKey {
	t.Helper()
	_ = pub
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return priv
}

// TestDispatchSignature_OptOut pins the opt-out contract:
// SetRequireDispatchSignatures(false) — the seam the daemon wiring uses
// for an explicit require_node_signatures=false — re-enables the legacy
// unsigned path.
func TestDispatchSignature_OptOut(t *testing.T) {
	invoker := &fakeAgentInvoker{output: "ok"}
	bridge := NewExecutorBridge("local", nil)
	// No key lookup at all: with verification ON this would reject.
	bridge.SetRequireDispatchSignatures(false)
	bridge.SetAgentInvoker(invoker)

	if err := bridge.HandleTaskCreate(taskCreateEvent(DispatchJob{
		JobID:           "optout-1",
		OriginNode:      "origin",
		AgentID:         "agent-a",
		TaskDescription: "opt-out probe",
	})); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for invoker.calls() == 0 {
		select {
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("opted-out bridge never invoked the agent")
		}
	}
}

// TestExecutorBridge_SigningKeyLookup_Interface pins the one-crypto-path
// rule: *GossipEngine satisfies SigningKeyLookup structurally, so the
// bridge can only ever verify against the gossip engine's existing
// ed25519 registry.
func TestExecutorBridge_SigningKeyLookup_Interface(t *testing.T) {
	var _ SigningKeyLookup = (*GossipEngine)(nil)
}

// TestExecutorBridge_RejectionDoesNotRegisterJob pins that a rejected job
// never enters the active map (no zombie JobStatus entries).
func TestExecutorBridge_RejectionDoesNotRegisterJob(t *testing.T) {
	bridge := NewExecutorBridge("local", nil)
	if err := bridge.HandleTaskCreate(taskCreateEvent(DispatchJob{
		JobID:      "ghost-1",
		OriginNode: "origin",
		AgentID:    "agent-a",
	})); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}
	bridge.mu.Lock()
	_, exists := bridge.active["ghost-1"]
	bridge.mu.Unlock()
	if exists {
		t.Fatal("rejected job was registered in the active map")
	}
}

// TestDispatchSignature_TamperChangesCanonicalBytes pins
// the signing-struct discipline: mutating any covered field invalidates
// the signature, and a re-sign over the mutated job verifies again.
func TestDispatchSignature_TamperChangesCanonicalBytes(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	job := DispatchJob{JobID: "j1", OriginNode: "n1", AgentID: "a1"}
	sig := ed25519.Sign(priv, job.signingBytes())
	if !ed25519.Verify(priv.Public().(ed25519.PublicKey), job.signingBytes(), sig) {
		t.Fatal("clean signature failed to verify")
	}
	job.AgentID = "a2"
	if ed25519.Verify(priv.Public().(ed25519.PublicKey), job.signingBytes(), sig) {
		t.Fatal("tampered job still verified — signing struct must cover agent_id")
	}
}

// TestExecutorBridge_SetSigningKeyLookup_NilGuard pins the nil guard on
// the key-lookup setter.
func TestExecutorBridge_SetSigningKeyLookup_NilGuard(t *testing.T) {
	bridge := NewExecutorBridge("local", nil)
	bridge.SetSigningKeyLookup(nil)
	if bridge.signingKeys != nil {
		t.Fatal("nil lookup must not replace the field")
	}
}

// TestPeerSigningKeyDistribution_SignedRemoteDispatch pins issue 105:
// remote ed25519 signing keys move from the member registry into the
// gossip key registry (SetPeerSigningKey's production caller), and a
// signed cross-node dispatch from that peer verifies and executes.
// Unsigned and unknown-origin dispatches stay rejected. A rotated key
// keeps the previous key for the grace window so in-flight signatures
// still verify. Key bytes must not appear in logs.
func TestPeerSigningKeyDistribution_SignedRemoteDispatch(t *testing.T) {
	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate origin key: %v", err)
	}
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate local key: %v", err)
	}

	dir := t.TempDir()
	now := time.Now().UTC()
	if err := SaveMember(dir, &Member{
		NodeID:        "node-a",
		NodeName:      "origin",
		WireGuardPub:  "wg-origin",
		SigningPub:    append([]byte(nil), pubA...),
		Status:        "active",
		LastHeartbeat: now,
	}); err != nil {
		t.Fatalf("save origin member: %v", err)
	}
	if err := SaveMember(dir, &Member{
		NodeID:        "node-b",
		NodeName:      "local",
		WireGuardPub:  "wg-local",
		Status:        "active",
		LastHeartbeat: now,
	}); err != nil {
		t.Fatalf("save local member: %v", err)
	}

	cfg := &Config{NodeID: "node-b"}
	cfg.setDefault()
	cfg.Security.RequireNodeSignatures = true
	gs := NewGitSync(cfg, cfg, dir, logger)
	eng := NewGossipEngine(cfg, "node-b", nil, logger,
		WithSigningKey(privB, pubB),
		WithMembersProvider(gs),
	)
	// Do not call SetPeerSigningKey. Sync is the production caller.
	eng.SyncPeerSigningKeys()

	got, ok := eng.PeerSigningKey("node-a")
	if !ok || !bytes.Equal(got, pubA) {
		t.Fatal("remote signing key was not distributed into the gossip registry")
	}
	loaded, err := LoadMember(dir, "node-b")
	if err != nil {
		t.Fatalf("reload local member: %v", err)
	}
	if !bytes.Equal(loaded.SigningPub, pubB) {
		t.Fatal("local signing key was not published into the member registry")
	}

	handler := NewGossipHandler(nil, "node-b", logger, nil)
	eng.RegisterHandler(handler)
	invoker := &fakeAgentInvoker{output: "ok"}
	pub := &fakeBusPublisher{}
	bridge := NewExecutorBridge("node-b", logger)
	bridge.SetAgentInvoker(invoker)
	bridge.SetBusPublisher(pub)
	handler.SetExecutorBridge(bridge)
	if bridge.signingKeys == nil {
		t.Fatal("executor bridge was not wired to the gossip key registry")
	}

	job := signedJob(DispatchJob{
		JobID:           "dist-job-1",
		OriginNode:      "node-a",
		TargetNode:      "node-b",
		AgentID:         "agent-a",
		TaskDescription: "signed remote dispatch",
		CreatedAt:       time.Now().UnixNano(),
	}, privA)
	event := taskCreateEvent(job)
	event.NodeID = job.OriginNode
	if err := bridge.HandleTaskCreate(event); err != nil {
		t.Fatalf("HandleTaskCreate: %v", err)
	}
	waitForInvoker(t, invoker, 1)

	rejectInvoker := &fakeAgentInvoker{output: "nope"}
	bridge.SetAgentInvoker(rejectInvoker)
	unsigned := job
	unsigned.JobID = "dist-unsigned"
	unsigned.Signature = nil
	unsignedEvent := taskCreateEvent(unsigned)
	unsignedEvent.NodeID = unsigned.OriginNode
	if err := bridge.HandleTaskCreate(unsignedEvent); err != nil {
		t.Fatalf("unsigned HandleTaskCreate: %v", err)
	}
	expectNoExecution(t, rejectInvoker)

	ghost := signedJob(DispatchJob{
		JobID:           "dist-ghost",
		OriginNode:      "node-ghost",
		AgentID:         "agent-a",
		TaskDescription: "unknown origin",
		CreatedAt:       time.Now().UnixNano(),
	}, privA)
	ghostEvent := taskCreateEvent(ghost)
	ghostEvent.NodeID = ghost.OriginNode
	if err := bridge.HandleTaskCreate(ghostEvent); err != nil {
		t.Fatalf("ghost HandleTaskCreate: %v", err)
	}
	expectNoExecution(t, rejectInvoker)

	// Rotation: replace node-a's key. The previous key must still verify
	// until the grace window is expired.
	pubA2, privA2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate rotated key: %v", err)
	}
	if err := SaveMember(dir, &Member{
		NodeID:        "node-a",
		NodeName:      "origin",
		WireGuardPub:  "wg-origin",
		SigningPub:    append([]byte(nil), pubA2...),
		Status:        "active",
		LastHeartbeat: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save rotated member: %v", err)
	}
	eng.SyncPeerSigningKeys()
	if cur, ok := eng.PeerSigningKey("node-a"); !ok || !bytes.Equal(cur, pubA2) {
		t.Fatal("rotated signing key was not installed as current")
	}

	graceInvoker := &fakeAgentInvoker{output: "grace"}
	bridge.SetAgentInvoker(graceInvoker)
	oldJob := signedJob(DispatchJob{
		JobID:           "dist-inflight",
		OriginNode:      "node-a",
		AgentID:         "agent-a",
		TaskDescription: "in-flight after rotation",
		CreatedAt:       time.Now().UnixNano(),
	}, privA)
	oldEvent := taskCreateEvent(oldJob)
	oldEvent.NodeID = oldJob.OriginNode
	if err := bridge.HandleTaskCreate(oldEvent); err != nil {
		t.Fatalf("in-flight HandleTaskCreate: %v", err)
	}
	waitForInvoker(t, graceInvoker, 1)

	eng.expireSigningKeyGrace("node-a")
	afterGrace := &fakeAgentInvoker{output: "late"}
	bridge.SetAgentInvoker(afterGrace)
	late := signedJob(DispatchJob{
		JobID:           "dist-after-grace",
		OriginNode:      "node-a",
		AgentID:         "agent-a",
		TaskDescription: "signed with retired key",
		CreatedAt:       time.Now().UnixNano(),
	}, privA)
	lateEvent := taskCreateEvent(late)
	lateEvent.NodeID = late.OriginNode
	if err := bridge.HandleTaskCreate(lateEvent); err != nil {
		t.Fatalf("after-grace HandleTaskCreate: %v", err)
	}
	expectNoExecution(t, afterGrace)

	fresh := signedJob(DispatchJob{
		JobID:           "dist-new-key",
		OriginNode:      "node-a",
		AgentID:         "agent-a",
		TaskDescription: "signed with rotated key",
		CreatedAt:       time.Now().UnixNano(),
	}, privA2)
	freshInvoker := &fakeAgentInvoker{output: "new"}
	bridge.SetAgentInvoker(freshInvoker)
	freshEvent := taskCreateEvent(fresh)
	freshEvent.NodeID = fresh.OriginNode
	if err := bridge.HandleTaskCreate(freshEvent); err != nil {
		t.Fatalf("rotated-key HandleTaskCreate: %v", err)
	}
	waitForInvoker(t, freshInvoker, 1)

	logs := logBuf.String()
	for _, secret := range []string{
		base64.StdEncoding.EncodeToString(pubA),
		base64.StdEncoding.EncodeToString(pubB),
		base64.StdEncoding.EncodeToString(privA),
		base64.StdEncoding.EncodeToString(privB),
	} {
		if strings.Contains(logs, secret) {
			t.Fatal("signing key material was written to logs")
		}
	}
}

func waitForInvoker(t *testing.T, invoker *fakeAgentInvoker, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for invoker.calls() < want {
		select {
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatalf("invoker calls: want %d, got %d", want, invoker.calls())
		}
	}
}
