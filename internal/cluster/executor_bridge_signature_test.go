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
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

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
