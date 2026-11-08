package agent

// T7 unit arm (01-delegation/00T-tests.md): B6 descriptor JSON round-trip.
// Contract .hermes/contracts/01-delegation.md B6:
//   ChildDescriptor{Version int (==1), Mode, Provider, Label, AgentProvider,
//   AgentModel, Reasoning, ParentSessionID, SeedPayload string,
//   CreatedAt time.Time}
//   stamped onto ChildRecord.Descriptor at spawn; DelegateManager.Resume(ctx,
//   childID) re-spawns from the descriptor.
//
// RED until leaf 06-descriptor-resume lands (ChildDescriptor undefined).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/session"
)

func TestChildDescriptor_JSONRoundTrip(t *testing.T) {
	in := ChildDescriptor{
		Version:         1,
		Mode:            "spawn",
		Provider:        "local",
		Label:           "probe child",
		AgentProvider:   "local",
		AgentModel:      "lfm-1.2b",
		Reasoning:       "low",
		ParentSessionID: "sess-parent",
		SeedPayload:     "user: do the thing",
		CreatedAt:       time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC),
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal descriptor: %v", err)
	}

	var out ChildDescriptor
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal descriptor: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
	if out.Version != 1 {
		t.Errorf("Version = %d after round-trip, want 1", out.Version)
	}
}

func TestChildDescriptor_UnknownVersionRejected(t *testing.T) {
	// A descriptor stamped by a future build must be rejected, not silently
	// resumed with wrong semantics (same posture as session schema_version).
	raw := []byte(`{"version":2,"mode":"spawn","parent_session_id":"s"}`)
	var d ChildDescriptor
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := d.Validate(); err == nil {
		t.Error("Validate accepted an unknown descriptor version 2")
	}

	raw = []byte(`{"version":1,"mode":"spawn","parent_session_id":"s"}`)
	d = ChildDescriptor{}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal v1: %v", err)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Validate rejected a well-formed v1 descriptor: %v", err)
	}
}

// TestDelegateManager_DescriptorPersistResume proves the cold-resume wiring:
// a settled child's descriptor is persisted into the daemon-owned store, and a
// fresh DelegateManager (no live child) re-spawns it from that durable record
// under the SAME childID, respecting parent-only auth. Uses the in-memory
// session store so the test needs no SQLite fixture.
func TestDelegateManager_DescriptorPersistResume(t *testing.T) {
	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(nil, nil, nil)
	mgr.SetStore(store)

	ctx := context.Background()
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "seed message",
		Model: "lfm-1.2b", Provider: "local",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mgr.SettleForTest(res.ChildID, ChildStateCompleted, "done", "")

	// The durable store now holds the child descriptor.
	stored, err := store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("GetChildDescriptor: %v", err)
	}
	if stored.AgentID != "probe" || stored.ParentSessionID != "sess-parent" {
		t.Errorf("stored record = %+v, want agent probe / parent sess-parent", stored)
	}
	d, err := UnmarshalChildDescriptor(stored.DescriptorJSON)
	if err != nil {
		t.Fatalf("UnmarshalChildDescriptor(stored): %v", err)
	}
	if d.Mode != "spawn" || d.AgentModel != "lfm-1.2b" {
		t.Errorf("stored descriptor = %+v, want spawn/lfm-1.2b", d)
	}

	// Cold resume: a fresh manager holding only the store must NOT see the live
	// child, and must refuse a non-parent resumer.
	fresh := NewDelegateManager(nil, nil, nil)
	fresh.SetStore(store)
	if _, ok := fresh.Lookup(res.ChildID); ok {
		t.Fatal("fresh manager unexpectedly sees the live child")
	}
	if _, err := fresh.Resume(ctx, "sess-other", res.ChildID); !errors.Is(err, ErrResumeParentMismatch) {
		t.Fatalf("Resume by non-parent err = %v, want ErrResumeParentMismatch", err)
	}
	rr, err := fresh.Resume(ctx, "sess-parent", res.ChildID)
	if err != nil {
		t.Fatalf("Resume by parent: %v", err)
	}
	if rr.ChildID != res.ChildID || rr.Status != "running" {
		t.Errorf("Resume result = %+v, want child %s running", rr, res.ChildID)
	}
	// The durable record is flipped back to running by the resume.
	reloaded, err := store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("GetChildDescriptor after resume: %v", err)
	}
	if reloaded.State != string(ChildStateRunning) {
		t.Errorf("durable state after resume = %q, want running", reloaded.State)
	}
}

// TestDelegateManager_ResumeRejectsRunning proves a running child cannot be
// resumed (guard error), honoring the leaf's state machine.
func TestDelegateManager_ResumeRejectsRunning(t *testing.T) {
	mgr := NewDelegateManager(nil, nil, nil)
	res, err := mgr.Spawn(context.Background(), DelegateRequest{
		AgentID: "probe", Mode: "spawn", Message: "m",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, err := mgr.Resume(context.Background(), "sess-parent", res.ChildID); !errors.Is(err, ErrChildNotResumable) {
		t.Fatalf("Resume of running child err = %v, want ErrChildNotResumable", err)
	}
}
