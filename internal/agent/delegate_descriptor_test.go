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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

func TestChildDescriptor_JSONRoundTrip(t *testing.T) {
	in := ChildDescriptor{
		Version:         childDescriptorVersion,
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
	// SeedMessages (a slice) makes ChildDescriptor non-comparable; compare
	// field-wise, keeping the rest of the original equality posture.
	if out.Version != in.Version || out.Mode != in.Mode || out.Provider != in.Provider ||
		out.Label != in.Label || out.AgentProvider != in.AgentProvider || out.AgentModel != in.AgentModel ||
		out.Reasoning != in.Reasoning || out.ParentSessionID != in.ParentSessionID ||
		out.SeedPayload != in.SeedPayload || !out.CreatedAt.Equal(in.CreatedAt) {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) {
		t.Errorf("CreatedAt round-trip = %v, want %v", out.CreatedAt, in.CreatedAt)
	}
	if out.Version != childDescriptorVersion {
		t.Errorf("Version = %d after round-trip, want %d", out.Version, childDescriptorVersion)
	}
}

func TestChildDescriptor_UnknownVersionRejected(t *testing.T) {
	// A descriptor stamped by a future build must be rejected, not silently
	// resumed with wrong semantics (same posture as session schema_version).
	raw := []byte(`{"version":3,"mode":"spawn","parent_session_id":"s"}`)
	var d ChildDescriptor
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := d.Validate(); err == nil {
		t.Error("Validate accepted an unknown descriptor version 3")
	}

	// v1 stays readable (old-version pinning): it validates and simply
	// carries no seed — resuming it is brief-only, exactly as before.
	raw = []byte(`{"version":1,"mode":"spawn","parent_session_id":"s"}`)
	d = ChildDescriptor{}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal v1: %v", err)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Validate rejected a well-formed v1 descriptor: %v", err)
	}
	if len(d.SeedMessages) != 0 {
		t.Errorf("v1 descriptor carried %d seed messages, want none", len(d.SeedMessages))
	}

	// v2 validates and may carry the seed snapshot.
	raw = []byte(`{"version":2,"mode":"fork","parent_session_id":"s","seed_messages":[{"role":"user","content":"ctx"}]}`)
	d = ChildDescriptor{}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal v2: %v", err)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Validate rejected a well-formed v2 descriptor: %v", err)
	}
	if len(d.SeedMessages) != 1 || d.SeedMessages[0].Content != "ctx" {
		t.Errorf("v2 seed round-trip = %+v, want one 'ctx' message", d.SeedMessages)
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
	mgr.SettleForTest(res.ChildID, ChildStateInterrupted, "", "cancelled")

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
	// child, and must refuse a non-parent resumer. The child is settled
	// INTERRUPTED first (C5: a completed child is terminal and refused on the
	// cold path — see TestDelegateManager_ResumeRefusesCompletedChild).
	mgr.SettleForTest(res.ChildID, ChildStateInterrupted, "", "cancelled")
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

// TestChildDescriptor_CaptureSeedBounds pins the bounded capture: the newest
// tail of the seed is kept under both the message-count and byte-budget caps,
// an empty seed is a no-op, and a seed-bearing descriptor bumps to v2.
func TestChildDescriptor_CaptureSeedBounds(t *testing.T) {
	var d ChildDescriptor
	d.captureSeed(nil)
	if len(d.SeedMessages) != 0 {
		t.Fatal("nil seed must not populate SeedMessages")
	}

	// Message-count cap: 50 tiny messages -> exactly the last 40 kept, in
	// order, oldest-first.
	var seed []llm.ChatMessage
	for i := range seedDescriptorMaxMessages + 10 {
		seed = append(seed, llm.ChatMessage{Role: llm.RoleUser, Content: fmt.Sprintf("m%02d", i)})
	}
	d = ChildDescriptor{Version: 1}
	d.captureSeed(seed)
	if d.Version != childDescriptorVersion {
		t.Errorf("seed-bearing descriptor Version = %d, want %d", d.Version, childDescriptorVersion)
	}
	if len(d.SeedMessages) != seedDescriptorMaxMessages {
		t.Fatalf("captured %d messages, want %d", len(d.SeedMessages), seedDescriptorMaxMessages)
	}
	if got := d.SeedMessages[0].Content; got != fmt.Sprintf("m%02d", 10) {
		t.Errorf("first kept message = %q, want the oldest tail entry m10", got)
	}
	if got := d.SeedMessages[len(d.SeedMessages)-1].Content; got != fmt.Sprintf("m%02d", len(seed)-1) {
		t.Errorf("last kept message = %q, want the newest entry", got)
	}

	// Byte-budget cap: few messages but huge ones -> shrink until it fits.
	huge := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: strings.Repeat("x", seedDescriptorMaxBytes/3)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("y", seedDescriptorMaxBytes/3)},
		{Role: llm.RoleUser, Content: strings.Repeat("z", seedDescriptorMaxBytes/3)},
	}
	d = ChildDescriptor{Version: 2}
	d.captureSeed(huge)
	if size := seedJSONBytes(d.SeedMessages); size > seedDescriptorMaxBytes {
		t.Errorf("captured seed exceeds byte budget: %d > %d", size, seedDescriptorMaxBytes)
	}
	// Newest message survives the shrink.
	if n := len(d.SeedMessages); n == 0 || d.SeedMessages[n-1].Content[0] != 'z' {
		t.Error("byte-budget shrink dropped the newest seed message")
	}
}

// TestChildDescriptor_CapturedSeedRoundTrips proves the captured seed survives
// the durable JSON round-trip byte-for-message.
func TestChildDescriptor_CapturedSeedRoundTrips(t *testing.T) {
	seed := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "parent question"},
		{Role: llm.RoleAssistant, Content: "parent answer"},
	}
	d := ChildDescriptor{Version: childDescriptorVersion, Mode: "fork", ParentSessionID: "s"}
	d.captureSeed(seed)
	raw, err := MarshalChildDescriptor(d)
	if err != nil {
		t.Fatalf("MarshalChildDescriptor: %v", err)
	}
	out, err := UnmarshalChildDescriptor(raw)
	if err != nil {
		t.Fatalf("UnmarshalChildDescriptor: %v", err)
	}
	if len(out.SeedMessages) != len(seed) {
		t.Fatalf("round-trip kept %d seed messages, want %d", len(out.SeedMessages), len(seed))
	}
	for i := range seed {
		if out.SeedMessages[i].Role != seed[i].Role || out.SeedMessages[i].Content != seed[i].Content {
			t.Errorf("seed[%d] = %s/%q, want %s/%q", i,
				out.SeedMessages[i].Role, out.SeedMessages[i].Content, seed[i].Role, seed[i].Content)
		}
	}
}

// TestDelegateManager_ResumeStampsSeed proves the cold-resume seed carry: a
// fork child with a captured seed persists it in the descriptor, a fresh
// manager holding only the store restores it onto the record, and resumeChild
// re-stamps it through the seedWriter (plus the fork preamble) before the
// resumed child's first turn.
func TestDelegateManager_ResumeStampsSeed(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["fork-full"] = &AgentSpec{ID: "fork-full", Enabled: true, DelegateContext: "full"}
	reg.mu.Unlock()

	store := session.NewMemoryStore(nil)
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetForkSeedWriter(reg.ForkSeedWriter())
	mgr.SetStore(store)

	marker := "PARENT-MARKER-RESUME"
	ctx := tools.ContextWithConversation(context.Background(), parentConvWithMarker(marker))
	res, err := mgr.Spawn(ctx, DelegateRequest{
		AgentID: "fork-full", Message: "continue the analysis", Mode: "fork",
	}, "sess-parent")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, err := mgr.Join(context.Background(), "sess-parent", res.ChildID, 5*time.Second); err != nil {
		t.Fatalf("join: %v", err)
	}
	// The child settled FAILED (unwired RunAgent) in this rig and is already
	// settled, so SettleForTest no-ops. Rewrite the durable row to
	// INTERRUPTED through the real persist seam so the cold resume below is
	// permitted under the C5 terminal-state contract (only interrupted/
	// pending rows resume — a failed/completed row is refused).
	if e, ok := mgr.children[res.ChildID]; ok {
		e.rec.State = ChildStateInterrupted
	}
	if err := mgr.saveDescriptorToStore(mgr.children[res.ChildID].rec); err != nil {
		t.Fatalf("persist interrupted row: %v", err)
	}

	// The durable descriptor carries the seed snapshot.
	stored, err := store.GetChildDescriptor(ctx, res.ChildID)
	if err != nil {
		t.Fatalf("GetChildDescriptor: %v", err)
	}
	d, err := UnmarshalChildDescriptor(stored.DescriptorJSON)
	if err != nil {
		t.Fatalf("UnmarshalChildDescriptor: %v", err)
	}
	if len(d.SeedMessages) == 0 {
		t.Fatal("persisted descriptor carries no seed snapshot")
	}
	found := false
	for _, m := range d.SeedMessages {
		if strings.Contains(m.Content, marker) {
			found = true
		}
		if m.Content == "in-flight, not yet answered" {
			t.Fatal("in-flight parent turn leaked into the persisted seed")
		}
	}
	if !found {
		t.Fatal("persisted seed missing the parent's completed-turn marker")
	}

	// Cold resume on a fresh manager: the seed is restored onto the record
	// and re-stamped onto the child's conversation before its first turn.
	fresh := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	fresh.SetForkSeedWriter(reg.ForkSeedWriter())
	fresh.SetStore(store)
	if _, err := fresh.Resume(ctx, "sess-parent", res.ChildID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	rec, ok := fresh.Lookup(res.ChildID)
	if !ok {
		t.Fatalf("resumed child %s not registered", res.ChildID)
	}
	if len(rec.seed) != len(d.SeedMessages) {
		t.Fatalf("restored seed = %d messages, want %d", len(rec.seed), len(d.SeedMessages))
	}
	if _, err := fresh.Join(context.Background(), "sess-parent", res.ChildID, 5*time.Second); err != nil {
		t.Fatalf("join after resume: %v", err)
	}
	loop, err := reg.Get("fork-full")
	if err != nil {
		t.Fatalf("child loop: %v", err)
	}
	conv := loop.GetConversation(rec.ConversationID)
	if conv == nil {
		t.Fatal("resumed child conversation was not re-seeded")
	}
	seeded := conv.GetMessages()
	if len(seeded) != len(rec.seed) {
		t.Fatalf("re-seeded conversation has %d messages, want %d", len(seeded), len(rec.seed))
	}
	if got := seeded[len(seeded)-1].Content; !strings.Contains(got, marker) {
		t.Fatalf("re-seeded conversation's last message missing marker: %q", got)
	}
}

// TestResumeChild_RestampsSeed pins resumeChild's seeded path: with a restored
// seed the seedWriter runs with the record's snapshot before the run; a
// seed-less resume never invokes the writer.
func TestResumeChild_RestampsSeed(t *testing.T) {
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	reg.mu.Lock()
	reg.specs["probe"] = &AgentSpec{ID: "probe", Enabled: true}
	reg.mu.Unlock()

	var seededOn []string
	var seededMsgs []llm.ChatMessage
	mgr := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr.SetForkSeedWriter(func(agentID, conversationID string, seed []llm.ChatMessage) {
		seededOn = append(seededOn, agentID+"|"+conversationID)
		seededMsgs = seed
	})

	rec := ChildRecord{
		ChildID: "delegate-restamp", AgentID: "probe", ParentSessionID: "s",
		Mode: "fork", ConversationID: id.Generate("delegate-"),
		State: ChildStateRunning,
		seed:  []llm.ChatMessage{{Role: llm.RoleUser, Content: "inherited turn"}},
	}
	mgr.children[rec.ChildID] = &childEntry{rec: rec, settle: make(chan struct{})}
	mgr.resumeChild(context.Background(), rec.ChildID, rec, "the brief")
	if len(seededOn) != 1 || seededOn[0] != "probe|"+rec.ConversationID {
		t.Fatalf("seedWriter calls = %v, want exactly probe|%s", seededOn, rec.ConversationID)
	}
	if len(seededMsgs) != 1 || seededMsgs[0].Content != "inherited turn" {
		t.Fatalf("seedWriter received %+v, want the record's seed snapshot", seededMsgs)
	}

	// Seed-less resume: the writer must never run.
	mgr2 := NewDelegateManager(reg, func(string, any) {}, func(string) error { return nil })
	mgr2.SetForkSeedWriter(func(agentID, conversationID string, seed []llm.ChatMessage) {
		t.Error("seedWriter ran for a seed-less resume")
	})
	rec2 := ChildRecord{
		ChildID: "delegate-plain", AgentID: "probe", ParentSessionID: "s",
		Mode: "spawn", ConversationID: id.Generate("delegate-"),
		State: ChildStateRunning,
	}
	mgr2.children[rec2.ChildID] = &childEntry{rec: rec2, settle: make(chan struct{})}
	mgr2.resumeChild(context.Background(), rec2.ChildID, rec2, "plain payload")
}
