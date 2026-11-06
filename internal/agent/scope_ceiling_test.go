package agent

// Agent-groups leaf 05 T3 (docs/plans/20261030-agent-groups/
// 05-memory-ceiling.md): the NEVER-WIDENS pin. A group ceiling wider than
// the agent's own declaration must be INERT at every recall surface —
// loop tuple (feeds buildMemoryContext + the memory tools via
// ScopeIdentity) and the dispatcher chat-lane options. Mutation probe:
// remove the EffectiveMemoryScope narrowing (resolveScopeInputsWithCeiling
// -> plain resolveScopeInputs at both sites) and this file MUST go RED.

import (
	"context"
	"testing"

	memcfg "github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/tools"
)

func TestMemoryCeilingNeverWidens(t *testing.T) {
	// Loop surface: agent declares project; the group record claims a
	// GLOBAL ceiling. The effective max scope must remain project — the
	// wider ceiling is inert (C7: groups NEVER widen).
	l := NewAgentLoop("ceiling-pin", "/tmp", WithLoopLogger(silentLogger()))
	l.SetProjectID("proj-ceiling-pin")
	l.mu.Lock()
	l.spec = &AgentSpec{
		ID:             "ceiling-pin-agent",
		Role:           RoleExecutor,
		Enabled:        true,
		MemoryMaxScope: string(memory.ScopeProject),
	}
	l.groupResolution = &groups.Resolved{
		Managed:        true,
		MemoryMaxScope: string(memory.ScopeGlobal),
	}
	l.mu.Unlock()

	maxScope, _, _, _ := l.resolveMemoryScope()
	if maxScope != memory.ScopeProject {
		t.Fatalf("T3 RED: a global group ceiling WIDENED a project-declared "+
			"agent: effective=%q, want project (C7: groups never widen)",
			maxScope)
	}

	// Same shape through the memory-tool identity: the ScopeIdentity the
	// loop injects into tool contexts (memory_store / memory_search /
	// memory_recall / memory_get_context all filter from it) must carry
	// the same narrowed scope.
	id := l.memoryScopeCtxIdentity()
	if id.MaxScope != string(memory.ScopeProject) {
		t.Fatalf("T3 RED: the memory-tool ScopeIdentity was widened by the "+
			"group ceiling: MaxScope=%q, want project", id.MaxScope)
	}

	// Dispatcher chat-lane surface: the scope options must stay at
	// project. The options are probed through the committed C2 predicate
	// (memory.FilterResults): a project-scope row bound to the loop's
	// project must stay VISIBLE to a project-effective caller — had the
	// ceiling widened the caller to global, the row would vanish (the C2
	// matrix: a global caller sees only global rows).
	reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
	if err := reg.RegisterSpec(l.spec); err != nil {
		t.Fatalf("register spec: %v", err)
	}
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: memcfg.MemoryConfig{
			Backend:  memcfg.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: memcfg.EpisodicConfig{Enabled: true},
		},
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init memory manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	// The dispatcher reads the session identity from the session store; a
	// stub reader pins the session to the probe project (same identity the
	// daemon binds from the session row).
	d := &Dispatcher{
		registry:         reg,
		memoryMgr:        mgr,
		multiUserEnabled: false,
		sessionStore:     stubSessionReader{id: "sess-ceiling", projectID: "proj-ceiling-pin"},
	}
	opts := d.sessionScopeOptions("ceiling-pin-agent", "sess-ceiling")
	if len(opts) == 0 {
		t.Fatal("T3 RED: sessionScopeOptions produced no scope options (identity lost)")
	}
	probe := memory.Memory{
		Content:  "ceiling probe row",
		Metadata: map[string]any{"scope": string(memory.ScopeProject), "project_id": "proj-ceiling-pin"},
	}
	if len(memory.FilterResults([]memory.MemoryResult{memory.MemoryToResult(probe)}, opts...)) != 1 {
		t.Fatal("T3 RED: the dispatcher lane no longer recalls project rows — " +
			"the global group ceiling widened the effective caller scope past " +
			"project (C7: groups never widen)")
	}
}

// stubSessionReader is a minimal SessionStoreReader returning one fixed
// session row (the dispatcher's session-identity lookup shape).
type stubSessionReader struct {
	id        string
	projectID string
}

func (s stubSessionReader) Get(id string) *session.Session {
	if id != s.id {
		return nil
	}
	return &session.Session{ID: s.id, ProjectID: s.projectID}
}

func (s stubSessionReader) GetByConversationID(id string) *session.Session {
	return s.Get(id)
}

// TestMemoryCeilingNarrowsGroupedAgent pins the narrowing half (T2) at the
// loop surface: agent declares global (chief-of-staff posture), the group
// record carries project => effective is project.
func TestMemoryCeilingNarrowsGroupedAgent(t *testing.T) {
	l := NewAgentLoop("ceiling-narrow", "/tmp", WithLoopLogger(silentLogger()))
	l.mu.Lock()
	l.spec = &AgentSpec{
		ID:             "ceiling-narrow-agent",
		Role:           RoleExecutor,
		Enabled:        true,
		MemoryMaxScope: string(memory.ScopeGlobal),
	}
	l.groupResolution = &groups.Resolved{
		Managed:        true,
		MemoryMaxScope: string(memory.ScopeProject),
	}
	l.mu.Unlock()

	maxScope, _, _, _ := l.resolveMemoryScope()
	if maxScope != memory.ScopeProject {
		t.Fatalf("T2 RED: a project group ceiling did NOT narrow a "+
			"global-declared agent: effective=%q, want project", maxScope)
	}

	id := l.memoryScopeCtxIdentity()
	if id.MaxScope != string(memory.ScopeProject) {
		t.Fatalf("T2 RED: the memory-tool ScopeIdentity ignored the group "+
			"ceiling: MaxScope=%q, want project", id.MaxScope)
	}
}

// TestMemoryCeilingUngroupedBaseline pins the ungrouped invariance leg:
// a nil group record resolves byte-identically to the post-#70 baseline
// (resolveScopeInputs on the same inputs).
func TestMemoryCeilingUngroupedBaseline(t *testing.T) {
	l := NewAgentLoop("ceiling-ungrouped", "/tmp", WithLoopLogger(silentLogger()))
	l.mu.Lock()
	l.spec = &AgentSpec{
		ID:             "ceiling-ungrouped-agent",
		Role:           RoleExecutor,
		Enabled:        true,
		MemoryMaxScope: string(memory.ScopeGlobal),
	}
	// groupResolution nil = no group policy bound.
	l.mu.Unlock()

	lMax, lProj, lUID, lMulti := l.resolveMemoryScope()
	bMax, bProj, bUID, bMulti := resolveScopeInputs(l.spec, l.GetProjectID(), l.scopeUserID)
	if lMax != bMax || lProj != bProj || lUID != bUID || lMulti != bMulti {
		t.Fatalf("T2 RED: ungrouped resolution drifted from the post-#70 "+
			"baseline: got (%v,%q,%q,%v) want (%v,%q,%q,%v)",
			lMax, lProj, lUID, lMulti, bMax, bProj, bUID, bMulti)
	}
}

// TestMemoryToolIdentityCarriesCeiling closes the memory-tools leg of T2:
// the identity injected via executeToolCallsUngated (tools.ScopeIdentity)
// is the narrowed tuple, so the four memory tools filter at the ceiling
// without their own resolution path.
func TestMemoryToolIdentityCarriesCeiling(t *testing.T) {
	l := NewAgentLoop("ceiling-tools", "/tmp", WithLoopLogger(silentLogger()))
	l.mu.Lock()
	l.spec = &AgentSpec{
		ID:             "ceiling-tools-agent",
		Role:           RoleExecutor,
		Enabled:        true,
		MemoryMaxScope: string(memory.ScopeUser),
	}
	l.groupResolution = &groups.Resolved{
		Managed:        true,
		MemoryMaxScope: string(memory.ScopeProject),
	}
	l.mu.Unlock()

	id := tools.ScopeIdentity(l.memoryScopeCtxIdentity())
	if id.MaxScope != string(memory.ScopeProject) {
		t.Fatalf("memory tools would filter at %q; the group ceiling requires "+
			"project (C7)", id.MaxScope)
	}
}
