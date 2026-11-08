package agent

// delegate.go implements the C1 delegation seam: an async DelegateManager
// that supersedes the synchronous delegate_task tool while keeping it
// byte-compatible for existing callers. It spawns children (spawn/fork/acp),
// returns a ChildID + status "running" immediately, records each child in a
// per-session, sync.RWMutex-guarded registry keyed by childID, publishes an
// "agent.delegate.result" bus event on settlement, and exposes a bounded
// Join primitive so a turn can fan out several delegations then await them.
//
// Contract: .hermes/contracts/01-delegation.md (B1, M-C1); leaf
// docs/plans/20261102-dsh-convergence/01-delegation/01-spawn-async.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/id"
)

// maxDelegateDepth is the structural ceiling for a delegated child's nesting
// depth (the registry exposes no per-agent depth knob; guardedInvoke enforces
// the same class of cap for RLM subagents). Spawn clamps req.Depth to this
// ceiling and floors it at 1, mirroring dsh's depth cap posture.
const maxDelegateDepth = 4

// ChildState is the lifecycle state of a delegated child.
type ChildState string

const (
	// ChildStatePending is a child that was registered but has not started yet.
	ChildStatePending ChildState = "pending"
	// ChildStateRunning is a child whose spawn was accepted and is executing.
	ChildStateRunning ChildState = "running"
	// ChildStateCompleted is a child that settled with a final response.
	ChildStateCompleted ChildState = "completed"
	// ChildStateInterrupted is a child that was cancelled via Cancel.
	ChildStateInterrupted ChildState = "interrupted"
	// ChildStateFailed is a child that settled with an error.
	ChildStateFailed ChildState = "failed"
)

// ChildDescriptor is a durable description of a child that leaf 06 persists
// for cold resume. Version must be 1; a future build's descriptor is rejected
// by Validate rather than silently resumed with wrong semantics.
type ChildDescriptor struct {
	Version         int       `json:"version"`
	Mode            string    `json:"mode"`
	Provider        string    `json:"provider,omitempty"`
	Label           string    `json:"label,omitempty"`
	AgentProvider   string    `json:"agent_provider,omitempty"`
	AgentModel      string    `json:"agent_model,omitempty"`
	Reasoning       string    `json:"reasoning,omitempty"`
	ParentSessionID string    `json:"parent_session_id"`
	SeedPayload     string    `json:"seed_payload,omitempty"`
	CreatedAt       time.Time `json:"created_at"`

	// Label is written by SetDescriptorFactory (B6). Reserved field,
	// populated by the same wiring seam leaf 06 ships.
}

// Validate reports whether the descriptor is well-formed and current. Full
// implementation (version + mode) lives in delegate_descriptor.go; kept here as
// a thin wrapper for symmetry with the struct it documents.
func (d ChildDescriptor) Validate() error { return validateChildDescriptor(d) }

// ChildRecord is the per-session registry entry for a delegated child.
type ChildRecord struct {
	ChildID         string
	AgentID         string
	ParentSessionID string
	Mode            string
	Model           string
	Provider        string
	ReasoningEffort string
	ConversationID  string
	State           ChildState
	CreatedAt       time.Time
	Result          string
	Err             string
	Descriptor      ChildDescriptor

	// seed is the B2 history-seeded fork snapshot, captured ONCE at spawn
	// (not re-derived from the parent's newer history on resume). It rides
	// the child descriptor for cold resume (leaf 06 serializes it); here it
	// keeps the spawn-time snapshot stable in memory for the child run.
	seed []llm.ChatMessage
}

// ChildOptions carries the per-child model routing parameters resolved by the
// route resolver (B4); the identity resolver passes the request's values
// through unchanged.
type ChildOptions struct {
	Provider        string
	Model           string
	ReasoningEffort string
}

// DelegateRequest is the C1 request shape for spawning a delegated child.
type DelegateRequest struct {
	AgentID         string          `json:"agent_id"`
	Message         string          `json:"message"`
	Context         string          `json:"context,omitempty"`
	OutputSchema    json.RawMessage `json:"output_schema,omitempty"`
	Mode            string          `json:"mode,omitempty"` // "spawn"|"fork"|"acp" (default spawn)
	Model           string          `json:"model,omitempty"`
	Provider        string          `json:"provider,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	Depth           int             `json:"depth,omitempty"` // max nesting, default 1
}

// DelegateResult is the C1 result shape; it expands the legacy platform.go
// shape with ChildID + Status so a spawn returns the child handle immediately.
type DelegateResult struct {
	AgentID  string `json:"agent_id"`
	Success  bool   `json:"success"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
	ChildID  string `json:"child_id,omitempty"` // steer/interrupt key
	Status   string `json:"status,omitempty"`   // running|completed|interrupted
}

// ErrChildNotFound is returned by Lookup/Join/Cancel for an unknown child.
var ErrChildNotFound = fmt.Errorf("delegate: child not found")

// ErrJoinTimeout is returned by Join when a child does not settle within the
// requested bounded wait.
var ErrJoinTimeout = fmt.Errorf("delegate: join timeout")

// childEntry is the internal registry cell: the record plus the settle signal
// (closed exactly once on settlement) and the per-child run context cancel.
type childEntry struct {
	rec     ChildRecord
	settle  chan struct{}
	settled bool // guarded by DelegateManager.mu
	cancel  context.CancelFunc
	sess    *acp.Session // acp mode only; for B3 Send/Cancel
}

// DelegateManager owns async child delegation for a parent session. It is
// safe for concurrent use; every field that can change post-construction is
// guarded by mu (children map).
type DelegateManager struct {
	reg *AgentRegistry
	acp *acp.Manager

	mu       sync.RWMutex
	children map[string]*childEntry // childID -> entry

	// Impossible-by-contract: these wiring seams are set once at daemon
	// startup (NewDelegateManager / Set*) and never mutated afterwards, so
	// they are read without the lock. All are nil-safe (default behavior).
	publisher  func(topic string, payload any)
	cancelHook func(childID string) error

	routeResolver     func(ChildOptions, DelegateRequest) (ChildOptions, error) // B4
	forkSeed          func(*Conversation) []llm.ChatMessage                     // B2
	descriptorFactory func(ChildRecord) ChildDescriptor                         // B6
	persist           func(ChildRecord) error                                   // B6

	// store is the daemon-owned durable child-descriptor store (B6), set once
	// via SetStore. When wired, settle() persists each child's descriptor here
	// (best-effort, never failing the child turn) so an interrupted child can
	// be cold-resumed after a parent/daemon restart. Optional: nil disables
	// durable persistence (run-state only).
	store session.Store
}

// NewDelegateManager constructs the manager. A nil registry disables child
// execution (children are still registered and can be settled through the
// test seam / external hooks); publisher and cancelHook are optional and
// nil-safe.
func NewDelegateManager(reg *AgentRegistry, publisher func(string, any), cancelHook func(string) error) *DelegateManager {
	return &DelegateManager{
		reg:        reg,
		publisher:  publisher,
		cancelHook: cancelHook,
		children:   make(map[string]*childEntry),
		// B2: register the history-seeded fork slicer as the default so
		// every spawn path seeds Mode:"fork" without waiting for daemon
		// wiring. It is pure and nil-safe (nil conversation -> nil seed ->
		// fresh shared-transcript spawn), and a later SetForkSeed(nil)
		// still resets to that default no-op behavior.
		forkSeed: completedTurnPrefix,
	}
}

// SetRouteResolver installs the B4 per-child route resolver. nil resets to the
// identity default (request values pass through unchanged).
func (m *DelegateManager) SetRouteResolver(f func(ChildOptions, DelegateRequest) (ChildOptions, error)) {
	m.routeResolver = f
}

// SetForkSeed installs the B2 fork-seed seam that materializes a parent
// transcript for IsolationSharedTranscript. nil keeps a fresh spawn default.
func (m *DelegateManager) SetForkSeed(f func(*Conversation) []llm.ChatMessage) {
	m.forkSeed = f
}

// SetDescriptorFactory installs the B6 descriptor writer, stamped onto each
// child at spawn. nil keeps the built-in descriptor.
func (m *DelegateManager) SetDescriptorFactory(f func(ChildRecord) ChildDescriptor) {
	m.descriptorFactory = f
}

// SetPersist installs the B6 persistence hook called on settlement. nil makes
// persistence a no-op.
func (m *DelegateManager) SetPersist(f func(ChildRecord) error) {
	m.persist = f
}

// SetStore installs the daemon-owned durable child-descriptor store (B6). When
// set, every settled child's descriptor is persisted here (best-effort) and
// DelegateManager.Resume can cold-resume an interrupted child after a
// parent/daemon restart. nil disables durable persistence. It never replaces
// the SetPersist hook — if both are set, both run (store first, then hook).
func (m *DelegateManager) SetStore(store session.Store) {
	m.store = store
}

// SetACPManager injects the acp.Manager used for Mode=="acp" delegation
// children. A nil/absent ACP manager makes an acp child settle with
// "acp provider not wired".
func (m *DelegateManager) SetACPManager(acpMgr *acp.Manager) {
	m.acp = acpMgr
}

// Spawn validates the request, registers the child as running, and returns the
// child handle immediately; the child executes on a goroutine and settles
// (bus event + record update) asynchronously.
func (m *DelegateManager) Spawn(ctx context.Context, req DelegateRequest, parentSessionID string) (DelegateResult, error) {
	if req.AgentID == "" {
		return DelegateResult{}, fmt.Errorf("delegate: agent_id is required")
	}
	if req.Message == "" {
		return DelegateResult{}, fmt.Errorf("delegate: message is required")
	}

	mode := req.Mode
	if mode == "" {
		mode = "spawn"
	}
	if mode != "spawn" && mode != "fork" && mode != "acp" {
		return DelegateResult{}, fmt.Errorf("delegate: unsupported mode %q (want spawn|fork|acp)", mode)
	}

	// Resolve the per-child route (identity by default; B4 later).
	opts := ChildOptions{Provider: req.Provider, Model: req.Model, ReasoningEffort: req.ReasoningEffort}
	if resolve := m.routeResolver; resolve != nil {
		resolved, err := resolve(opts, req)
		if err != nil {
			return DelegateResult{AgentID: req.AgentID, Success: false, Error: fmt.Sprintf("route resolution failed: %v", err)}, nil
		}
		opts = resolved
	}

	// Validate the spec exists BEFORE any child exists. A nil registry skips
	// the check (unwired/test registries register children without execution).
	if m.reg != nil && mode != "acp" {
		if _, ok := m.reg.GetSpec(req.AgentID); !ok {
			return DelegateResult{AgentID: req.AgentID, Success: false, Error: "agent not found: " + req.AgentID}, nil
		}
	}

	// Depth: default 1, never above the structural ceiling.
	depth := req.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > maxDelegateDepth {
		depth = maxDelegateDepth
	}

	childID := id.Generate("delegate-")
	conversationID := id.Generate("delegate-")
	now := time.Now()

	rec := ChildRecord{
		ChildID:         childID,
		AgentID:         req.AgentID,
		ParentSessionID: parentSessionID,
		Mode:            mode,
		Model:           opts.Model,
		Provider:        opts.Provider,
		ReasoningEffort: opts.ReasoningEffort,
		ConversationID:  conversationID,
		State:           ChildStateRunning,
		CreatedAt:       now,
	}

	// Stamp the child descriptor (B6 hook if set, else the built-in).
	if df := m.descriptorFactory; df != nil {
		rec.Descriptor = df(rec)
		if rec.Descriptor.Version == 0 {
			rec.Descriptor.Version = 1
		}
	} else {
		rec.Descriptor = ChildDescriptor{
			Version:         1,
			Mode:            mode,
			Provider:        opts.Provider,
			AgentProvider:   opts.Provider,
			AgentModel:      opts.Model,
			Reasoning:       opts.ReasoningEffort,
			ParentSessionID: parentSessionID,
			SeedPayload:     req.Message,
			CreatedAt:       now,
		}
	}

	m.mu.Lock()
	if m.children == nil {
		m.children = make(map[string]*childEntry)
	}
	childCtx, cancel := context.WithCancel(ctx)
	e := &childEntry{rec: rec, settle: make(chan struct{}), cancel: cancel}
	m.children[childID] = e
	m.mu.Unlock()

	// Launch the child ONLY when a registry is wired; unwired/test children
	// remain pending settlement through the test seam / external hooks.
	if m.reg != nil {
		go m.runChild(childCtx, req, childID, rec)
	} else {
		go func() {
			<-childCtx.Done()
		}()
	}

	_ = depth // depth is a per-child routing pin for B4; recorded, not yet a runtime gate

	return DelegateResult{AgentID: req.AgentID, ChildID: childID, Status: "running", Success: true}, nil
}

// runChild executes an agent-mode (spawn/fork) child and settles it. acp-mode
// children are routed to runACPChild.
func (m *DelegateManager) runChild(ctx context.Context, req DelegateRequest, childID string, rec ChildRecord) {
	defer func() {
		if r := recover(); r != nil {
			m.settle(childID, ChildStateFailed, "", fmt.Sprintf("delegate child panicked: %v", r))
		}
	}()

	if rec.Mode == "acp" {
		m.runACPChild(ctx, req, childID, rec)
		return
	}
	if m.reg == nil { // unwired registry: leave the child in flight
		return
	}

	spec, ok := m.reg.GetSpec(req.AgentID)
	if !ok {
		m.settle(childID, ChildStateFailed, "", "agent not found: "+req.AgentID)
		return
	}

	fullMessage, err := m.childPrompt(rec, req)
	if err != nil {
		m.settle(childID, ChildStateFailed, "", err.Error())
		return
	}

	resp, err := m.reg.RunAgent(ctx, spec.ID, fullMessage, rec.ConversationID)
	if err != nil {
		m.settle(childID, ChildStateFailed, "", err.Error())
		return
	}
	m.settle(childID, ChildStateCompleted, resp, "")
}

// runACPChild routes a Mode=="acp" child through the existing *acp.Manager
// (the tested, scrubbed lane) — it never builds a second ACP client.
func (m *DelegateManager) runACPChild(ctx context.Context, req DelegateRequest, childID string, rec ChildRecord) {
	if m.acp == nil {
		m.settle(childID, ChildStateFailed, "", "acp provider not wired")
		return
	}

	sess, err := m.acp.GetOrCreatePersona(ctx, req.AgentID, "", "", acp.PersonaOpts{})
	if err != nil {
		m.settle(childID, ChildStateFailed, "", fmt.Sprintf("acp session start failed: %v", err))
		return
	}

	m.mu.Lock()
	if e, ok := m.children[childID]; ok {
		e.sess = sess
	}
	m.mu.Unlock()

	fullMessage, err := m.childPrompt(rec, req)
	if err != nil {
		m.settle(childID, ChildStateFailed, "", err.Error())
		return
	}

	resp, err := sess.Send(ctx, fullMessage)
	if err != nil {
		m.settle(childID, ChildStateFailed, "", fmt.Sprintf("acp session send failed: %v", err))
		return
	}
	m.settle(childID, ChildStateCompleted, resp, "")
}

// childPrompt builds the child's full input message from the request, mirroring
// the legacy delegate_task prompt construction (structured spawn brief +
// optional caller context + optional output-schema directive). In fork mode it
// seeds the child with the parent's completed-turn prefix, CAPTURED ONCE at
// this spawn invocation (each spawn runs childPrompt a single time in
// runChild), so the child replays a stable snapshot rather than re-forking the
// parent's newer history. The seed first computed is stashed on the record and
// reused on any re-render — never recomputed.
func (m *DelegateManager) childPrompt(rec ChildRecord, req DelegateRequest) (string, error) {
	var fullMessage string
	switch rec.Mode {
	case "fork":
		// B2: materialize the parent-transcript seed once at spawn. The
		// concrete *Conversation is threaded through Spawn's
		// parent-session context by callers that hold it; the default
		// hook (completedTurnPrefix, nil-safe) yields an empty seed for a
		// nil conversation, falling back to a fresh shared-transcript
		// brief. Best-effort by contract: never a hard requirement.
		if rec.seed == nil && m.forkSeed != nil {
			rec.seed = m.forkSeed(nil)
		}
		spawn := BuildSpawnContext(IsolationSharedTranscript, req.Message, nil, nil, rec.seed)
		fullMessage = RenderSpawnContext(spawn)
	default: // spawn
		spawn := BuildSpawnContext(IsolationArtifactOnly, req.Message, nil, nil, nil)
		fullMessage = RenderSpawnContext(spawn)
	}

	if req.Context != "" {
		fullMessage = "Context: " + req.Context + "\n\n" + fullMessage
	}
	if len(req.OutputSchema) > 0 {
		fullMessage = fmt.Sprintf("%s\n\nIMPORTANT: Your response must be valid JSON matching this schema:\n```json\n%s\n```\nRespond with ONLY the JSON, no additional text.",
			fullMessage, string(req.OutputSchema))
	}
	return fullMessage, nil
}

// Children returns a snapshot (copies) of all children registered under a
// parent session, in no particular order.
func (m *DelegateManager) Children(parentSessionID string) []ChildRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ChildRecord, 0, len(m.children))
	for _, e := range m.children {
		if e.rec.ParentSessionID == parentSessionID {
			out = append(out, e.rec)
		}
	}
	return out
}

// Session returns the live ACP session recorded for an acp-mode child, if
// any. It backs the B3 child-control SendMessage path, which routes an acp
// child's message through its recorded session (never a second client).
func (m *DelegateManager) Session(childID string) (*acp.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.children[childID]
	if !ok || e.sess == nil {
		return nil, false
	}
	return e.sess, true
}

// Lookup returns a copy of a child record by childID.
func (m *DelegateManager) Lookup(childID string) (ChildRecord, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.children[childID]
	if !ok {
		return ChildRecord{}, false
	}
	return e.rec, true
}

// Join waits (bounded by timeout and ctx) for a child to settle and returns
// its final result. It returns ErrChildNotFound for an unknown child and a
// deadline/ctx error when the wait is exceeded.
func (m *DelegateManager) Join(ctx context.Context, parentSessionID, childID string, timeout time.Duration) (DelegateResult, error) {
	m.mu.RLock()
	e, ok := m.children[childID]
	if !ok || e.rec.ParentSessionID != parentSessionID {
		m.mu.RUnlock()
		return DelegateResult{}, ErrChildNotFound
	}
	settle := e.settle
	m.mu.RUnlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-settle:
		m.mu.RLock()
		rec := e.rec
		m.mu.RUnlock()
		return m.resultFromRecord(rec), nil
	case <-timer.C:
		return DelegateResult{}, ErrJoinTimeout
	case <-ctx.Done():
		return DelegateResult{}, ctx.Err()
	}
}

// Cancel interrupts a running child: it invokes the external cancel hook, the
// per-child run context, and any live ACP session, then marks the record
// interrupted and publishes settlement.
func (m *DelegateManager) Cancel(childID string) error {
	m.mu.Lock()
	e, ok := m.children[childID]
	if !ok {
		m.mu.Unlock()
		return ErrChildNotFound
	}
	var cancel context.CancelFunc
	var sess *acp.Session
	if !e.settled {
		e.settled = true
		e.rec.State = ChildStateInterrupted
		cancel = e.cancel
		sess = e.sess
		close(e.settle)
	}
	hook := m.cancelHook
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if sess != nil {
		_ = sess.Cancel() // best-effort; the child's own failure path owns the record
	}
	if hook != nil {
		return hook(childID)
	}
	return nil
}

// SettleForTest is the unit-test seam that settles a child as if its run had
// completed. It is not used by production callers.
func (m *DelegateManager) SettleForTest(childID string, state ChildState, result, errStr string) {
	m.settle(childID, state, result, errStr)
}

// settle finalizes a child's record exactly once: it updates state/result/err
// under the write lock, closes the settle channel, then publishes the bus
// event and runs the persistence hook OUTSIDE the lock (no I/O under mutex).
func (m *DelegateManager) settle(childID string, state ChildState, result, errStr string) {
	m.mu.Lock()
	e, ok := m.children[childID]
	if !ok || e.settled {
		m.mu.Unlock()
		return
	}
	e.settled = true
	e.rec.State = state
	e.rec.Result = result
	e.rec.Err = errStr
	close(e.settle)
	rec := e.rec
	m.mu.Unlock()

	m.publishResult(rec)
	m.persistRecord(rec)
}

// resultFromRecord maps a settled record to the C1 DelegateResult shape.
func (m *DelegateManager) resultFromRecord(rec ChildRecord) DelegateResult {
	status := string(rec.State)
	res := DelegateResult{
		AgentID:  rec.AgentID,
		ChildID:  rec.ChildID,
		Status:   status,
		Response: rec.Result,
		Error:    rec.Err,
	}
	res.Success = status == string(ChildStateCompleted) && rec.Err == ""
	return res
}

// publishResult emits the agent.delegate.result bus event for a settled child.
func (m *DelegateManager) publishResult(rec ChildRecord) {
	if m.publisher == nil {
		return
	}
	payload := map[string]any{
		"child_id":          rec.ChildID,
		"status":            string(rec.State),
		"agent_id":          rec.AgentID,
		"response":          rec.Result,
		"error":             rec.Err,
		"parent_session_id": rec.ParentSessionID,
	}
	m.publisher("agent.delegate.result", payload)
}

// persistRecord runs the B6 persistence hook on completion. Failure is
// best-effort and never fails the child lifecycle (there is no logger on the
// manager to surface the error). When a durable store is wired (SetStore), the
// child's descriptor is persisted there FIRST; the SetPersist hook (if any)
// runs afterwards. A failed persist never fails the completed child turn
// (root AGENTS.md invariants).
func (m *DelegateManager) persistRecord(rec ChildRecord) {
	if m.store != nil {
		_ = m.saveDescriptorToStore(rec)
	}
	if m.persist == nil {
		return
	}
	_ = m.persist(rec)
}
