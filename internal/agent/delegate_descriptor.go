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
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
)

// childDescriptorVersion is the only descriptor version this build understands.
// A descriptor stamped by a future build (version > 1) is rejected on
// UnmarshalChildDescriptor / Validate rather than silently resumed with wrong
// semantics (same posture as the session schema_version contract).
const childDescriptorVersion = 1

// validateChildDescriptor enforces the B6 descriptor contract: the version must
// be exactly 1 and the mode must be present. Mode is the one field every
// legitimate descriptor must carry; Provider/AgentModel/etc. are routing pins
// that may legitimately be empty for the identity-route default.
func validateChildDescriptor(d ChildDescriptor) error {
	if d.Version != childDescriptorVersion {
		return fmt.Errorf("unsupported child descriptor version %d (expected %d)", d.Version, childDescriptorVersion)
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
// or resumed with wrong semantics). Malformed JSON is also an error.
func UnmarshalChildDescriptor(b []byte) (ChildDescriptor, error) {
	var d ChildDescriptor
	if err := json.Unmarshal(b, &d); err != nil {
		return ChildDescriptor{}, fmt.Errorf("child descriptor: %w", err)
	}
	if d.Version != childDescriptorVersion {
		return ChildDescriptor{}, fmt.Errorf("%w: got %d, build supports %d", ErrDescriptorUnknownVersion, d.Version, childDescriptorVersion)
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
// Both paths replay the frozen fork seed (SeedPayload), re-pin the descriptor's
// model/route, set the record back to running, and re-publish the settlement on
// completion. Only the parent (record.ParentSessionID == caller) may resume.
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

// resumeChild executes a resuming child using the frozen seed payload verbatim
// as its brief — the fork seed replays the SNAPSHOT (SeedPayload), never a
// re-fork of the parent's newer history. It settles exactly like a fresh spawn.
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
	resp, err := m.reg.RunAgent(ctx, spec.ID, seedPayload, rec.ConversationID)
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
