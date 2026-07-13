package agent

import (
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/session"
	"log/slog"
	"time"
)

// managerConfig returns a minimal, ready-to-use ManagerConfig for tests.
func managerConfig() ManagerConfig {
	return ManagerConfig{
		SessionStore: session.NewMemoryStore(slog.Default()),
	}
}

func TestManager_GetOrCreate_New(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	loop, err := mgr.GetOrCreate("sess-new", "/tmp/workdir")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop")
	}

	// Subsequent GetOrCreate returns the same loop (identity)
	loop2, err := mgr.GetOrCreate("sess-new", "/tmp/workdir")
	if err != nil {
		t.Fatalf("second GetOrCreate failed: %v", err)
	}
	if loop2 != loop {
		t.Error("expected GetOrCreate to return the same loop for existing session")
	}
}

func TestManager_GetExisting(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// Get returns false for non-existent session
	_, ok := mgr.Get("does-not-exist")
	if ok {
		t.Error("expected Get to return false for non-existent session")
	}

	// Create a session via GetOrCreate
	_, err := mgr.GetOrCreate("sess-getme", "/tmp/test")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Get is read-only: does not create
	loop, ok := mgr.Get("sess-getme")
	if !ok {
		t.Error("expected Get to return true for existing session")
	}
	if loop == nil {
		t.Fatal("expected non-nil loop from Get")
	}

	// Get never creates — verify by calling Get before a GetOrCreate
	_, ok = mgr.Get("sess-creates-no")
	if ok {
		t.Error("Get should not create sessions")
	}
	// Now create it via GetOrCreate (not Get — that's the whole point)
	_, err = mgr.GetOrCreate("sess-creates-no", "/tmp")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
}

func TestManager_Remove(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	_, err := mgr.GetOrCreate("sess-remove", "/tmp/test")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Verify it exists
	if _, ok := mgr.Get("sess-remove"); !ok {
		t.Fatal("expected session to exist before Remove")
	}

	mgr.Remove("sess-remove")

	// Verify removal
	if _, ok := mgr.Get("sess-remove"); ok {
		t.Error("expected session to be removed")
	}

	// Remove is idempotent (no panic on missing key)
	mgr.Remove("sess-remove")

	// Can create new loop with same sessionID after remove
	loop, err := mgr.GetOrCreate("sess-remove", "/tmp/other")
	if err != nil {
		t.Fatalf("GetOrCreate after Remove failed: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop after re-Create")
	}
}

func TestManager_List(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// Empty manager returns empty map
	loops := mgr.List()
	if len(loops) != 0 {
		t.Errorf("expected 0 loops, got %d", len(loops))
	}

	// After creating N loops, List returns N
	for i := 1; i <= 4; i++ {
		_, err := mgr.GetOrCreate("sess-list-"+string(rune('a'+i-1)), "/tmp/test")
		if err != nil {
			t.Fatalf("GetOrCreate [%d] failed: %v", i, err)
		}
	}

	loops = mgr.List()
	if len(loops) != 4 {
		t.Errorf("expected 4 loops, got %d", len(loops))
	}
}

func TestManager_Concurrent(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	var wg sync.WaitGroup
	const goroutines = 100
	var errs sync.Mutex
	var firstErr error

	// Goroutines target a small set of session IDs so some contention
	// occurs (10 unique IDs × 10 goroutines each).
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(bucket int) {
			defer wg.Done()
			sid := "sess-concurrent-" + string(rune('a'+bucket%10))
			_, err := mgr.GetOrCreate(sid, "/tmp/workdir")
			if err != nil {
				errs.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errs.Unlock()
			}
		}(g)
	}

	wg.Wait()

	if firstErr != nil {
		t.Fatalf("concurrent error: %v", firstErr)
	}

	// Verify consistency: List should reflect all 10 session IDs (not more,
	// since many goroutines targeted the same bucket).
	loops := mgr.List()
	if len(loops) != 10 {
		t.Errorf("expected 10 unique loops, got %d", len(loops))
	}
}

func TestManager_Validation(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	tests := []struct {
		name       string
		sessionID  string
		workingDir string
		wantErr    string
	}{
		{"empty sessionID", "", "/tmp/test", "sessionID required"},
		{"empty workingDir", "sess-1", "", "workingDir required"},
		{"both empty", "", "", "sessionID required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.GetOrCreate(tt.sessionID, tt.workingDir)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- GetOrCreateWired tests ---

// makeTemplateLoop creates a minimal template loop with distinctive config
// values for ConfigSnapshot / GetOrCreateWired testing.
func makeTemplateLoop(t *testing.T) *AgentLoop {
	t.Helper()
	template := NewAgentLoop("daemon-template", "/template/dir")
	cfg := DefaultAgentConfig()
	cfg.MaxIterations = 4242
	cfg.Timeout = 99 * time.Second
	cfg.Purpose = "template-purpose-marker"
	cfg.GlobalRules = "template-global-rules"
	template.SetConfig(cfg)
	return template
}

func TestManager_GetOrCreateWired_InheritsConfig(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())
	template := makeTemplateLoop(t)

	loop, err := mgr.GetOrCreateWired("sess-wired", "/project/dir", template)
	if err != nil {
		t.Fatalf("GetOrCreateWired failed: %v", err)
	}

	cfg := loop.GetConfig()
	if cfg.MaxIterations != 4242 {
		t.Errorf("MaxIterations = %d, want 4242", cfg.MaxIterations)
	}
	if cfg.Timeout != 99*time.Second {
		t.Errorf("Timeout = %v, want 99s", cfg.Timeout)
	}
	if cfg.Purpose != "template-purpose-marker" {
		t.Errorf("Purpose = %q, want %q", cfg.Purpose, "template-purpose-marker")
	}
	if cfg.GlobalRules != "template-global-rules" {
		t.Errorf("GlobalRules = %q, want %q", cfg.GlobalRules, "template-global-rules")
	}
}

func TestManager_GetOrCreateWired_IdentityOnReuse(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())
	template := makeTemplateLoop(t)

	loop1, err := mgr.GetOrCreateWired("sess-identity", "/project/a", template)
	if err != nil {
		t.Fatalf("first GetOrCreateWired failed: %v", err)
	}

	loop2, err := mgr.GetOrCreateWired("sess-identity", "/project/b", template)
	if err != nil {
		t.Fatalf("second GetOrCreateWired failed: %v", err)
	}

	if loop1 != loop2 {
		t.Error("expected same loop pointer on reuse")
	}
}

func TestManager_GetOrCreateWired_ValidationErrors(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())
	template := makeTemplateLoop(t)

	tests := []struct {
		name       string
		sessionID  string
		workingDir string
		template   *AgentLoop
		wantErr    string
	}{
		{"empty sessionID", "", "/dir", template, "sessionID required"},
		{"empty workingDir", "sess-1", "", template, "workingDir required"},
		{"nil template", "sess-1", "/dir", nil, "template loop required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.GetOrCreateWired(tt.sessionID, tt.workingDir, tt.template)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestManager_GetOrCreateWired_IndependentSessionContext(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())
	template := makeTemplateLoop(t)

	loop, err := mgr.GetOrCreateWired("sess-isolated", "/unique/project/path", template)
	if err != nil {
		t.Fatalf("GetOrCreateWired failed: %v", err)
	}

	if loop.GetSessionID() == template.GetSessionID() {
		t.Errorf("sessionID = %q, should differ from template %q",
			loop.GetSessionID(), template.GetSessionID())
	}
	if loop.GetWorkingDir() == template.GetWorkingDir() {
		t.Errorf("workingDir = %q, should differ from template %q",
			loop.GetWorkingDir(), template.GetWorkingDir())
	}
	if loop.GetSessionID() != "sess-isolated" {
		t.Errorf("sessionID = %q, want %q", loop.GetSessionID(), "sess-isolated")
	}
	if loop.GetWorkingDir() != "/unique/project/path" {
		t.Errorf("workingDir = %q, want %q", loop.GetWorkingDir(), "/unique/project/path")
	}
}

func TestManager_GetOrCreateWired_DoesNotMutateTemplate(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())
	template := makeTemplateLoop(t)

	origSessionID := template.GetSessionID()
	origWorkingDir := template.GetWorkingDir()
	origConfig := template.GetConfig()

	_, err := mgr.GetOrCreateWired("sess-no-mutate", "/other/dir", template)
	if err != nil {
		t.Fatalf("GetOrCreateWired failed: %v", err)
	}

	if template.GetSessionID() != origSessionID {
		t.Errorf("template sessionID mutated: %q -> %q",
			origSessionID, template.GetSessionID())
	}
	if template.GetWorkingDir() != origWorkingDir {
		t.Errorf("template workingDir mutated: %q -> %q",
			origWorkingDir, template.GetWorkingDir())
	}
	afterCfg := template.GetConfig()
	if afterCfg.MaxIterations != origConfig.MaxIterations {
		t.Errorf("template MaxIterations mutated: %d -> %d",
			origConfig.MaxIterations, afterCfg.MaxIterations)
	}
	if afterCfg.Purpose != origConfig.Purpose {
		t.Errorf("template Purpose mutated: %q -> %q",
			origConfig.Purpose, afterCfg.Purpose)
	}
}

func TestAgentLoop_ConfigSnapshot(t *testing.T) {
	t.Parallel()

	template := NewAgentLoop("daemon-snapshot", "/snap/dir")
	cfg := DefaultAgentConfig()
	cfg.MaxIterations = 7777
	cfg.Purpose = "snapshot-test-purpose"
	template.SetConfig(cfg)

	opts := template.ConfigSnapshot()
	if len(opts) == 0 {
		t.Fatal("ConfigSnapshot returned empty slice")
	}

	// Apply snapshot to a fresh loop and verify config was inherited.
	fresh := NewAgentLoop("fresh-session", "/fresh/dir", opts...)
	freshCfg := fresh.GetConfig()
	if freshCfg.MaxIterations != 7777 {
		t.Errorf("fresh MaxIterations = %d, want 7777", freshCfg.MaxIterations)
	}
	if freshCfg.Purpose != "snapshot-test-purpose" {
		t.Errorf("fresh Purpose = %q, want %q", freshCfg.Purpose, "snapshot-test-purpose")
	}

	// Verify session-specific fields are NOT inherited.
	if fresh.GetSessionID() == template.GetSessionID() {
		t.Error("ConfigSnapshot should not propagate sessionID")
	}
	if fresh.GetWorkingDir() == template.GetWorkingDir() {
		t.Error("ConfigSnapshot should not propagate workingDir")
	}
}

// --- LoopsForTask tests ---

// setTaskID is a test helper that sets a loop's currentTaskID under its mutex.
// Only used in tests within the agent package.
func setTaskID(t *testing.T, loop *AgentLoop, taskID string) {
	t.Helper()
	loop.mu.Lock()
	defer loop.mu.Unlock()
	loop.currentTaskID = taskID
}

func TestManager_LoopsForTask(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// Create 3 loops across 2 agents/sessions and 2 taskIDs.
	// sess-A -> task-A
	// sess-B -> task-A
	// sess-C -> task-B
	loopA, err := mgr.GetOrCreate("sess-A", "/tmp/work")
	if err != nil {
		t.Fatalf("GetOrCreate sess-A: %v", err)
	}
	loopB, err := mgr.GetOrCreate("sess-B", "/tmp/work")
	if err != nil {
		t.Fatalf("GetOrCreate sess-B: %v", err)
	}
	loopC, err := mgr.GetOrCreate("sess-C", "/tmp/work")
	if err != nil {
		t.Fatalf("GetOrCreate sess-C: %v", err)
	}

	setTaskID(t, loopA, "task-A")
	setTaskID(t, loopB, "task-A")
	setTaskID(t, loopC, "task-B")

	// LoopsForTask("task-A") should return exactly loopA and loopB.
	matches := mgr.LoopsForTask("task-A")
	if len(matches) != 2 {
		t.Fatalf("expected 2 loops for task-A, got %d", len(matches))
	}

	// Verify identity: both loopA and loopB should be in the result.
	seen := make(map[*AgentLoop]bool)
	for _, l := range matches {
		seen[l] = true
	}
	if !seen[loopA] {
		t.Error("loopA not found in LoopsForTask(task-A) results")
	}
	if !seen[loopB] {
		t.Error("loopB not found in LoopsForTask(task-A) results")
	}
	if seen[loopC] {
		t.Error("loopC should NOT be in LoopsForTask(task-A) results")
	}

	// LoopsForTask("task-B") should return exactly loopC.
	matchesB := mgr.LoopsForTask("task-B")
	if len(matchesB) != 1 {
		t.Fatalf("expected 1 loop for task-B, got %d", len(matchesB))
	}
	if matchesB[0] != loopC {
		t.Error("expected loopC for task-B")
	}
}

func TestManager_LoopsForTask_NoMatches(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// No loops at all — should return nil.
	if got := mgr.LoopsForTask("nonexistent"); got != nil {
		t.Errorf("expected nil for empty manager, got %v", got)
	}

	// Create a loop with task-A and query for task-X.
	loop, err := mgr.GetOrCreate("sess-1", "/tmp/work")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	setTaskID(t, loop, "task-A")

	if got := mgr.LoopsForTask("task-X"); got != nil {
		t.Errorf("expected nil for unmatched taskID, got %v", got)
	}
}

func TestManager_LoopsForTask_EmptyTaskID(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// Empty taskID is a no-op — returns nil without scanning.
	loop, err := mgr.GetOrCreate("sess-1", "/tmp/work")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	setTaskID(t, loop, "")

	if got := mgr.LoopsForTask(""); got != nil {
		t.Errorf("expected nil for empty taskID, got %v", got)
	}
}
