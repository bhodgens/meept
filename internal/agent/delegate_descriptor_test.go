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
	"encoding/json"
	"testing"
	"time"
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
