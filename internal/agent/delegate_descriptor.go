package agent

// delegate_descriptor.go implements the B6 durable child descriptor and the
// cold-resume path (01-delegation leaf 06). A ChildDescriptor is a versioned,
// JSON-durable snapshot of a delegated child (mode/provider/model/reasoning/
// persona/seed) stamped onto every spawn's ChildRecord and persisted through
// the daemon-owned session store. DelegateManager.Resume re-spawns an
// interrupted/idle child from its durable descriptor — replaying its frozen
// fork seed and re-pinning its model/route — without loading a live Agent.
//
// Contract: .hermes/contracts/01-delegation.md B6; leaf
// docs/plans/20261102-dsh-convergence/01-delegation/06-descriptor-resume.md.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
)

// childDescriptorVersion is the only descriptor version this build understands.
// A descriptor stamped by a future build (version > 2) is rejected on
// UnmarshalChildDescriptor / Validate rather than silently resumed with wrong
// semantics (same posture as the session schema_version contract).
//
// Version history:
//
//	1 — original B6 shape (SeedPayload brief only, no fork-seed snapshot).
//	2 — adds SeedMessages: the bounded parent-context fork seed captured at
//	    spawn, so cold resume replays the seed a v1 descriptor lost. v1
//	    descriptors remain valid and resume brief-only (identical to pre-v2
//	    behavior), matching the version-pinning pattern: old versions stay
//	    READABLE, only future versions are rejected.
const childDescriptorVersion = 2

// Descriptor seed bounds. The fork seed can be large (the parent's whole
// completed-turn transcript); the durable descriptor must stay bounded, so the
// capture keeps at most the LAST seedDescriptorMaxMessages messages under
// seedDescriptorMaxBytes total (newest context first — the tail of the
// transcript is what orients the child). Bounds are descriptor-local constants
// (no config knob): consistent with the descriptor's existing fixed-shape
// posture, and the in-memory record keeps the FULL seed for the live run.
const (
	seedDescriptorMaxMessages = 40
	seedDescriptorMaxBytes    = 128 * 1024
)

// captureSeed materializes the spawn-time fork seed into the descriptor
// (bounded; newest-tail kept). nil/empty seed is a no-op (spawn-mode and
// degraded-fork children carry no seed).
func (d *ChildDescriptor) captureSeed(seed []llm.ChatMessage) {
	if len(seed) == 0 {
		return
	}
	msgs := append([]llm.ChatMessage(nil), seed...)
	// Keep the newest messages under both caps: drop from the front (oldest)
	// until the message count and total byte budget fit.
	for len(msgs) > seedDescriptorMaxMessages {
		msgs = msgs[1:]
	}
	for len(msgs) > 0 && seedJSONBytes(msgs) > seedDescriptorMaxBytes {
		msgs = msgs[1:]
	}
	if len(msgs) == 0 {
		return
	}
	d.SeedMessages = msgs
	if d.Version < childDescriptorVersion {
		d.Version = childDescriptorVersion
	}
}

// seedJSONBytes sizes a seed snapshot the way it will be serialized (JSON
// encode), so the byte budget matches the durable payload, not a heuristic.
func seedJSONBytes(msgs []llm.ChatMessage) int {
	raw, err := json.Marshal(msgs)
	if err != nil {
		// Unencodable seed can never be persisted; report oversize so the
		// shrink loop drops messages until it empties.
		return seedDescriptorMaxBytes + 1
	}
	return len(raw)
}

// validateChildDescriptor enforces the B6 descriptor contract: the version
// must be a readable one (1 = pre-seed, 2 = carries the bounded fork-seed
// snapshot) and the mode must be present. Mode is the one field every
// legitimate descriptor must carry; Provider/AgentModel/etc. are routing pins
// that may legitimately be empty for the identity-route default.
func validateChildDescriptor(d ChildDescriptor) error {
	if d.Version < 1 || d.Version > childDescriptorVersion {
		return fmt.Errorf("unsupported child descriptor version %d (build supports 1..%d)", d.Version, childDescriptorVersion)
	}
	if d.Mode == "" {
		return fmt.Errorf("child descriptor: mode is required")
	}
	return nil
}

// MarshalChildDescriptor serializes a descriptor to its durable JSON form. It is
// strict: an invalid descriptor (wrong version or missing mode) refuses to
// marshal so a corrupt record can never be persisted.
func MarshalChildDescriptor(d ChildDescriptor) ([]byte, error) {
	if err := validateChildDescriptor(d); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// ErrDescriptorUnknownVersion reports a descriptor from an unsupported (newer)
// build. It is returned by UnmarshalChildDescriptor so callers can distinguish
// a version mismatch from malformed JSON.
var ErrDescriptorUnknownVersion = fmt.Errorf("child descriptor: unsupported version")

// UnmarshalChildDescriptor rehydrates a descriptor from its durable JSON form.
// It is strict: an unknown/newer version is an error (never silently downgraded
// or resumed with wrong semantics). Older-but-readable versions (v1) unmarshal
// fine — they simply carry no SeedMessages and resume brief-only. Malformed
// JSON is also an error.
func UnmarshalChildDescriptor(b []byte) (ChildDescriptor, error) {
	var d ChildDescriptor
	if err := json.Unmarshal(b, &d); err != nil {
		return ChildDescriptor{}, fmt.Errorf("child descriptor: %w", err)
	}
	if d.Version < 1 || d.Version > childDescriptorVersion {
		return ChildDescriptor{}, fmt.Errorf("%w: got %d, build supports 1..%d", ErrDescriptorUnknownVersion, d.Version, childDescriptorVersion)
	}
	return d, nil
}

// descriptorFromRecord normalizes a ChildRecord into a durable ChildDescriptor,
// filling any gaps from the record so a descriptor factory that left a field
// empty still yields a persistable, valid descriptor. The seed payload is taken
// from the record's Descriptor when present (capture-once, B2), else falls back
// to the recorded result/mode as available.
func descriptorFromRecord(rec ChildRecord) ChildDescriptor {
	d := rec.Descriptor
	if d.Version == 0 {
		d.Version = childDescriptorVersion
	}
	if d.Mode == "" {
		d.Mode = rec.Mode
	}
	if d.ParentSessionID == "" {
		d.ParentSessionID = rec.ParentSessionID
	}
	if d.Provider == "" && rec.Provider != "" {
		d.Provider = rec.Provider
	}
	if d.AgentProvider == "" && rec.Provider != "" && rec.Mode != "acp" {
		d.AgentProvider = rec.Provider
	}
	if d.AgentModel == "" {
		d.AgentModel = rec.Model
	}
	if d.Reasoning == "" {
		d.Reasoning = rec.ReasoningEffort
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = rec.CreatedAt
	}
	return d
}

// saveDescriptorToStore persists a child's descriptor into the durable
// daemon-owned store. It is best-effort: a validation or store error is
// returned (and ignored by the settle path) but never fails the child turn.
func (m *DelegateManager) saveDescriptorToStore(rec ChildRecord) error {
	if m.store == nil {
		return nil
	}
	d := descriptorFromRecord(rec)
	raw, err := MarshalChildDescriptor(d)
	if err != nil {
		return fmt.Errorf("persist child %s descriptor: %w", rec.ChildID, err)
	}
	return m.store.SaveChildDescriptor(context.Background(), session.ChildDescriptorRecord{
		ChildID:         rec.ChildID,
		ParentSessionID: rec.ParentSessionID,
		AgentID:         rec.AgentID,
		State:           string(rec.State),
		DescriptorJSON:  raw,
		CreatedAt:       rec.CreatedAt,
	})
}

// ErrChildNotResumable is returned by Resume when the child's current state
// does not permit a cold resume (e.g. it is already running or completed).
var ErrChildNotResumable = errors.New("delegate: child not resumable")

// ErrResumeParentMismatch is returned by Resume when the caller is not the
// child's parent session. Auth contract: only the parent may resume a child.
var ErrResumeParentMismatch = errors.New("delegate: resume denied: caller is not the child's parent")

// Resume re-spawns an interrupted/idle child from its durable descriptor. It
// works in two layers:
//
//   - Live: the child still exists in this daemon run -> it must be in an
//     Interrupted (or Pending) state, and it is re-run under its recorded
//     descriptor.
//   - Cold: the child's record is gone from the run-state (parent/daemon
//     restart) -> it is rehydrated from the durable store, re-registered under
//     its original childID, and re-run from its persisted descriptor.
//
// Both paths replay the frozen fork seed — the descriptor's SeedMessages
// snapshot when present (v2, stamped onto the child's conversation before the
// first turn), else brief-only as v1 always did — re-pin the descriptor's
// model/route, set the record back to running, and re-publish the settlement
// on completion. Only the parent (record.ParentSessionID == caller) may resume.
func (m *DelegateManager) Resume(ctx context.Context, parentSessionID, childID string) (DelegateResult, error) {
	// Live path.
	if rec, ok := m.Lookup(childID); ok {
		return m.resumeRecord(ctx, parentSessionID, rec)
	}
	// Cold path: rehydrate from the durable store.
	if m.store == nil {
		return DelegateResult{}, ErrChildNotFound
	}
	stored, err := m.store.GetChildDescriptor(ctx, childID)
	if err != nil {
		if errors.Is(err, session.ErrChildDescriptorNotFound) {
			return DelegateResult{}, ErrChildNotFound
		}
		return DelegateResult{}, fmt.Errorf("delegate: resume: load %s: %w", childID, err)
	}
	d, err := UnmarshalChildDescriptor(stored.DescriptorJSON)
	if err != nil {
		return DelegateResult{}, fmt.Errorf("delegate: resume: %w", err)
	}
	rec := ChildRecord{
		ChildID:         stored.ChildID,
		ParentSessionID: stored.ParentSessionID,
		Mode:            d.Mode,
		Model:           d.AgentModel,
		Provider:        d.Provider,
		ReasoningEffort: d.Reasoning,
		ConversationID:  id.Generate("delegate-"),
		State:           ChildStatePending,
		CreatedAt:       stored.CreatedAt,
		Descriptor:      d,
	}
	// AgentID is not part of the descriptor shape; it is recovered from the
	// durable record (persisted alongside the descriptor).
	rec.AgentID = stored.AgentID
	return m.resumeRegister(ctx, parentSessionID, rec, d, stored.AgentID)
}

// resumeRegister registers a child under its original childID and launches the
// resume run. It is the shared tail of both the live and cold Resume paths.
func (m *DelegateManager) resumeRegister(ctx context.Context, parentSessionID string, rec ChildRecord, d ChildDescriptor, agentID string) (DelegateResult, error) {
	// Auth: only the parent may resume.
	if parentSessionID != "" && rec.ParentSessionID != parentSessionID {
		return DelegateResult{}, ErrResumeParentMismatch
	}
	payload := d.SeedPayload
	if payload == "" {
		return DelegateResult{}, fmt.Errorf("%w: %s has no seed payload to resume", ErrChildNotResumable, rec.ChildID)
	}
	// Cold-resume seed carry: a v2 descriptor holds the spawn-time fork seed,
	// so the resumed child replays the parent-context seed the original had
	// instead of only the brief. The restored snapshot rides the record like
	// a fresh spawn's; resumeChild re-stamps it through the seedWriter and
	// prepends the same fork preamble.
	if len(d.SeedMessages) > 0 {
		rec.seed = append([]llm.ChatMessage(nil), d.SeedMessages...)
		m.log().Debug("delegate resume: restored fork seed from descriptor",
			"child_id", rec.ChildID,
			"seed_messages", len(rec.seed),
			"seed_bytes", seedJSONBytes(rec.seed))
	}
	if agentID != "" {
		rec.AgentID = agentID
	}
	if rec.AgentID == "" {
		return DelegateResult{}, fmt.Errorf("%w: %s has no agent to resume", ErrChildNotResumable, rec.ChildID)
	}

	// Re-register / update to running under the SAME childID.
	m.mu.Lock()
	if _, exists := m.children[rec.ChildID]; exists && rec.AgentID == "" {
		// Unreachable in practice; guard keeps a stale entry from leaking.
		m.mu.Unlock()
		return DelegateResult{}, fmt.Errorf("delegate: resume: child %s already tracked", rec.ChildID)
	}
	ctx2, cancel := context.WithCancel(ctx)
	e := &childEntry{rec: rec, settle: make(chan struct{}), cancel: cancel}
	e.rec.State = ChildStateRunning
	m.children[rec.ChildID] = e
	m.mu.Unlock()

	// Mark the durable record running (best-effort; the settle path overwrites).
	if m.store != nil {
		dRunning := rec.Descriptor
		dRunning.Version = childDescriptorVersion
		_ = m.store.SaveChildDescriptor(ctx, session.ChildDescriptorRecord{
			ChildID:         rec.ChildID,
			ParentSessionID: rec.ParentSessionID,
			State:           string(ChildStateRunning),
			DescriptorJSON:  mustDescriptorJSON(dRunning, rec),
			CreatedAt:       rec.CreatedAt,
		})
	}

	if m.reg != nil {
		go m.resumeChild(ctx2, rec.ChildID, rec, payload)
	} else {
		go func() { <-ctx2.Done() }()
	}
	return DelegateResult{AgentID: rec.AgentID, ChildID: rec.ChildID, Status: "running", Success: true}, nil
}

// resumeRecord is the live-path Resume tail: validates the child's state
// (Interrupted/Pending only) and re-reads its descriptor.
func (m *DelegateManager) resumeRecord(ctx context.Context, parentSessionID string, rec ChildRecord) (DelegateResult, error) {
	switch rec.State {
	case ChildStateInterrupted, ChildStatePending:
		// resumable
	default:
		return DelegateResult{}, fmt.Errorf("%w: %s is in state %s", ErrChildNotResumable, rec.ChildID, rec.State)
	}
	// Auth checked inside resumeRegister (shared with cold path).
	return m.resumeRegister(ctx, parentSessionID, rec, descriptorFromRecord(rec), rec.AgentID)
}

// resumeChild executes a resuming child. It replays the frozen seed snapshot
// (SeedMessages, restored from the descriptor onto the record by
// resumeRegister) exactly like a fresh spawn's runChild: the conversation is
// re-stamped via the seedWriter and the fork preamble precedes the brief —
// the resume replays the SNAPSHOT, never a re-fork of the parent's newer
// history. It settles exactly like a fresh spawn.
func (m *DelegateManager) resumeChild(ctx context.Context, childID string, rec ChildRecord, seedPayload string) {
	defer func() {
		if r := recover(); r != nil {
			m.settle(childID, ChildStateFailed, "", fmt.Sprintf("delegate child resume panicked: %v", r))
		}
	}()
	if rec.Mode == "acp" {
		m.resumeACPChild(ctx, childID, rec, seedPayload)
		return
	}
	if m.reg == nil { // unwired registry: leave the child in flight
		return
	}
	spec, ok := m.reg.GetSpec(rec.AgentID)
	if !ok {
		m.settle(childID, ChildStateFailed, "", "agent not found: "+rec.AgentID)
		return
	}
	// Re-stamp the parent-context seed onto the child's conversation BEFORE
	// the first turn and prepend the fork preamble (same path as runChild).
	// The brief rides the recorded SeedPayload; the seed carries the parent
	// context. A seed-less resume (v1 descriptor / spawn-mode child) runs the
	// plain payload byte-identically.
	fullMessage := seedPayload
	if len(rec.seed) > 0 {
		if m.seedWriter != nil {
			m.seedWriter(spec.ID, rec.ConversationID, rec.seed)
		}
		fullMessage = forkSeedPreamble(len(rec.seed)) + fullMessage
	}
	resp, err := m.reg.RunAgent(ctx, spec.ID, fullMessage, rec.ConversationID)
	if err != nil {
		m.settle(childID, ChildStateFailed, "", err.Error())
		return
	}
	m.settle(childID, ChildStateCompleted, resp, "")
}

// resumeACPChild re-runs an acp-mode child through the recorded persona lane,
// sending the frozen seed payload.
func (m *DelegateManager) resumeACPChild(ctx context.Context, childID string, rec ChildRecord, seedPayload string) {
	if m.acp == nil {
		m.settle(childID, ChildStateFailed, "", "acp provider not wired")
		return
	}
	sess, err := m.acp.GetOrCreatePersona(ctx, rec.AgentID, "", "", acp.PersonaOpts{})
	if err != nil {
		m.settle(childID, ChildStateFailed, "", fmt.Sprintf("acp session start failed: %v", err))
		return
	}
	m.mu.Lock()
	if e, ok := m.children[childID]; ok {
		e.sess = sess
	}
	m.mu.Unlock()
	resp, err := sess.Send(ctx, seedPayload)
	if err != nil {
		m.settle(childID, ChildStateFailed, "", fmt.Sprintf("acp session send failed: %v", err))
		return
	}
	m.settle(childID, ChildStateCompleted, resp, "")
}

// Dispose tears down a child: it removes the durable descriptor record
// (best-effort) and, if the child is terminal, evicts its run-state entry. It
// is idempotent and safe on unknown children (returns ErrChildNotFound only for
// a wholly unknown child with no durable record). Designed for the delegation
// supervisor's explicit cleanup path after settlement.
func (m *DelegateManager) Dispose(ctx context.Context, parentSessionID, childID string) error {
	if m.store != nil {
		if err := m.store.RemoveChildDescriptor(ctx, childID); err != nil {
			return err
		}
	}
	m.mu.Lock()
	e, ok := m.children[childID]
	if !ok {
		m.mu.Unlock()
		if m.store == nil {
			return ErrChildNotFound
		}
		return nil
	}
	if parentSessionID != "" && e.rec.ParentSessionID != parentSessionID {
		m.mu.Unlock()
		return ErrResumeParentMismatch
	}
	delete(m.children, childID)
	m.mu.Unlock()
	return nil
}

// mustDescriptorJSON serializes a descriptor for the resume-time running-state
// persist, falling back to the normalized record descriptor on validation
// failure (best-effort).
func mustDescriptorJSON(d ChildDescriptor, rec ChildRecord) []byte {
	if raw, err := MarshalChildDescriptor(d); err == nil {
		return raw
	}
	raw, _ := MarshalChildDescriptor(descriptorFromRecord(rec))
	return raw
}
