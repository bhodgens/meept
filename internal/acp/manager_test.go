package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

func testACPConfig(enabled bool, maxAgents int) config.ACPConfig {
	return config.ACPConfig{
		Enabled:        enabled,
		AgentsFile:     "unused.json5",
		DialTimeout:    10,
		CallTimeout:    120,
		MaxAgents:      maxAgents,
		PermissionMode: "permissive",
	}
}

func testAgents(entries ...config.ACPAgentEntry) *config.ACPAgentsConfig {
	return &config.ACPAgentsConfig{Agents: entries}
}

type startRecorder struct {
	mu       sync.Mutex
	n        int
	configs  []SessionConfig
	sessions []*Session
	err      error
	delay    time.Duration
	hang     chan struct{} // if non-nil, start blocks until closed
}

func (r *startRecorder) start(_ context.Context, cfg SessionConfig) (*Session, error) {
	if r.hang != nil {
		<-r.hang
	}
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	r.configs = append(r.configs, cfg)
	if r.err != nil {
		return nil, r.err
	}
	s := &Session{state: StateReady}
	r.sessions = append(r.sessions, s)
	return s, nil
}

func (r *startRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func attachStart(m *Manager, r *startRecorder) {
	m.startFn = r.start
}

func TestManager_Disabled_EnabledFalse(t *testing.T) {
	t.Parallel()

	m := NewManager(testACPConfig(false, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"true"}, Enabled: true},
	))
	if m.Enabled() {
		t.Fatal("Enabled() = true, want false")
	}
}

func TestManager_Disabled_GetOrCreateReturnsErrDisabled(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(false, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"true"}, Enabled: true},
	))
	attachStart(m, rec)

	_, err := m.GetOrCreate(context.Background(), "echo", "/work")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("GetOrCreate error = %v, want ErrDisabled", err)
	}
	if rec.calls() != 0 {
		t.Fatalf("start called %d times, want 0 (disabled path must not spawn)", rec.calls())
	}
}

func TestManager_Disabled_StopAllNoop(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(false, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"true"}, Enabled: true},
	))
	attachStart(m, rec)

	m.StopAll() // must not panic
	if rec.calls() != 0 {
		t.Fatalf("start called %d times after StopAll, want 0", rec.calls())
	}
	if got := m.LiveSessions(); len(got) != 0 {
		t.Fatalf("LiveSessions = %v, want empty", got)
	}
}

func TestManager_GetOrCreate_CachesByAgentID(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	a, err := m.GetOrCreate(ctx, "echo", "/work")
	if err != nil {
		t.Fatalf("first GetOrCreate: %v", err)
	}
	b, err := m.GetOrCreate(ctx, "echo", "/other")
	if err != nil {
		t.Fatalf("second GetOrCreate: %v", err)
	}
	if a != b {
		t.Fatal("second GetOrCreate returned a different session")
	}
	if rec.calls() != 1 {
		t.Fatalf("start called %d times, want 1", rec.calls())
	}
}

func TestManager_GetOrCreate_ErrAgentNotFound(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	_, err := m.GetOrCreate(context.Background(), "missing", "/work")
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("error = %v, want ErrAgentNotFound", err)
	}
	if rec.calls() != 0 {
		t.Fatalf("start called %d times, want 0", rec.calls())
	}
}

func TestManager_GetOrCreate_ErrAgentDisabled(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "codex", Command: []string{"agent"}, Enabled: false},
	))
	attachStart(m, rec)

	_, err := m.GetOrCreate(context.Background(), "codex", "/work")
	if !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("error = %v, want ErrAgentDisabled", err)
	}
	if rec.calls() != 0 {
		t.Fatalf("start called %d times, want 0", rec.calls())
	}
}

func TestManager_GetOrCreate_ErrMaxAgents(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 1), testAgents(
		config.ACPAgentEntry{ID: "a", Command: []string{"a"}, Enabled: true},
		config.ACPAgentEntry{ID: "b", Command: []string{"b"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	if _, err := m.GetOrCreate(ctx, "a", "/work"); err != nil {
		t.Fatalf("first GetOrCreate: %v", err)
	}
	_, err := m.GetOrCreate(ctx, "b", "/work")
	if !errors.Is(err, ErrMaxAgents) {
		t.Fatalf("error = %v, want ErrMaxAgents", err)
	}
}

func TestManager_GetOrCreate_MaxAgentsConcurrent(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{delay: 20 * time.Millisecond}
	agents := make([]config.ACPAgentEntry, 5)
	ids := []string{"a1", "a2", "a3", "a4", "a5"}
	for i, id := range ids {
		agents[i] = config.ACPAgentEntry{ID: id, Command: []string{id}, Enabled: true}
	}
	m := NewManager(testACPConfig(true, 2), testAgents(agents...))
	attachStart(m, rec)

	var wg sync.WaitGroup
	var okCount, maxCount atomic.Int32
	errCh := make(chan error, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := m.GetOrCreate(context.Background(), id, "/work")
			switch {
			case err == nil:
				okCount.Add(1)
			case errors.Is(err, ErrMaxAgents):
				maxCount.Add(1)
			default:
				errCh <- err
			}
		}(id)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := okCount.Load(), int32(2); got != want {
		t.Errorf("successful GetOrCreate = %d, want %d", got, want)
	}
	if got, want := maxCount.Load(), int32(3); got != want {
		t.Errorf("ErrMaxAgents count = %d, want %d", got, want)
	}
	t.Cleanup(m.StopAll)
}

func TestManager_GetOrCreate_WorkdirCatalogCwdOverrides(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{
			ID:      "echo",
			Command: []string{"agent"},
			Cwd:     "/catalog/cwd",
			Enabled: true,
		},
	))
	attachStart(m, rec)

	if _, err := m.GetOrCreate(context.Background(), "echo", "/session/workdir"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.configs) != 1 {
		t.Fatalf("start configs = %d, want 1", len(rec.configs))
	}
	if got := rec.configs[0].Cwd; got != "/catalog/cwd" {
		t.Fatalf("SessionConfig.Cwd = %q, want catalog Cwd", got)
	}
}

func TestManager_GetOrCreate_EmptyCatalogCwdUsesWorkdirArg(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	const workdir = "/session/workdir"
	if _, err := m.GetOrCreate(context.Background(), "echo", workdir); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := rec.configs[0].Cwd; got != workdir {
		t.Fatalf("SessionConfig.Cwd = %q, want workdir argument %q", got, workdir)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if rec.configs[0].Cwd == wd {
		t.Fatal("SessionConfig.Cwd used process working directory")
	}
}

func TestManager_GetOrCreate_EmptyWorkdirDoesNotUseGetwd(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	if _, err := m.GetOrCreate(context.Background(), "echo", ""); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.configs[0].Cwd != "" {
		t.Fatalf("SessionConfig.Cwd = %q, want empty (must not call os.Getwd)", rec.configs[0].Cwd)
	}
}

func TestManager_GetOrCreate_PassesCatalogAndTimeouts(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{
			ID:          "echo",
			Command:     []string{"bin", "--acp"},
			Env:         map[string]string{"FOO": "bar"},
			DefaultMode: "read-only",
			Enabled:     true,
		},
	))
	attachStart(m, rec)

	if _, err := m.GetOrCreate(context.Background(), "echo", "/work"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	cfg := rec.configs[0]
	if cfg.AgentID != "echo" {
		t.Errorf("AgentID = %q, want echo", cfg.AgentID)
	}
	if len(cfg.Command) != 2 || cfg.Command[0] != "bin" || cfg.Command[1] != "--acp" {
		t.Errorf("Command = %v, want [bin --acp]", cfg.Command)
	}
	if cfg.Env["FOO"] != "bar" {
		t.Errorf("Env = %v, want FOO=bar", cfg.Env)
	}
	if cfg.DefaultMode != "read-only" {
		t.Errorf("DefaultMode = %q, want read-only", cfg.DefaultMode)
	}
	if cfg.DialTimeout != 10*time.Second {
		t.Errorf("DialTimeout = %s, want 10s", cfg.DialTimeout)
	}
	if cfg.CallTimeout != 120*time.Second {
		t.Errorf("CallTimeout = %s, want 120s", cfg.CallTimeout)
	}
	if cfg.PermissionMode != "permissive" {
		t.Errorf("PermissionMode = %q, want permissive", cfg.PermissionMode)
	}
}

func TestManager_Stop_RemovesAndCloses(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	sess, err := m.GetOrCreate(ctx, "echo", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if err := m.Stop("echo"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if sess.State() != StateClosed {
		t.Fatalf("State() = %v, want StateClosed", sess.State())
	}
	if got := m.LiveSessions(); len(got) != 0 {
		t.Fatalf("LiveSessions after Stop = %v, want empty", got)
	}

	again, err := m.GetOrCreate(ctx, "echo", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate after Stop: %v", err)
	}
	if again == sess {
		t.Fatal("GetOrCreate after Stop returned the closed session")
	}
	if rec.calls() != 2 {
		t.Fatalf("start called %d times, want 2", rec.calls())
	}
	t.Cleanup(m.StopAll)
}

func TestManager_StopAll_ClosesAll(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "a", Command: []string{"a"}, Enabled: true},
		config.ACPAgentEntry{ID: "b", Command: []string{"b"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	sa, err := m.GetOrCreate(ctx, "a", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate a: %v", err)
	}
	sb, err := m.GetOrCreate(ctx, "b", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate b: %v", err)
	}
	m.StopAll()
	m.StopAll() // idempotent
	if sa.State() != StateClosed || sb.State() != StateClosed {
		t.Fatalf("states after StopAll: a=%v b=%v, want both StateClosed", sa.State(), sb.State())
	}
	if got := m.LiveSessions(); len(got) != 0 {
		t.Fatalf("LiveSessions after StopAll = %v, want empty", got)
	}
}

func TestManager_LiveSessions(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	if got := m.LiveSessions(); got == nil || len(got) != 0 {
		t.Fatalf("LiveSessions before create = %v, want empty map", got)
	}
	sess, err := m.GetOrCreate(context.Background(), "echo", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	got := m.LiveSessions()
	if len(got) != 1 {
		t.Fatalf("LiveSessions len = %d, want 1", len(got))
	}
	if got["echo"] != sess.State() {
		t.Fatalf("LiveSessions[echo] = %v, want %v", got["echo"], sess.State())
	}
	got["echo"] = StateClosed
	again := m.LiveSessions()
	if again["echo"] == StateClosed && sess.State() != StateClosed {
		t.Fatal("LiveSessions returned a live map (caller mutation leaked)")
	}
	t.Cleanup(m.StopAll)
}

func TestManager_AgentsSnapshot(t *testing.T) {
	t.Parallel()

	catalog := testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Env: map[string]string{"K": "V"}, Enabled: true},
	)
	m := NewManager(testACPConfig(true, 3), catalog)
	catalog.Agents[0].ID = "mutated-source"

	got := m.Agents()
	if len(got) != 1 || got[0].ID != "echo" {
		t.Fatalf("Agents() after mutating source = %+v, want id=echo", got)
	}
	got[0].ID = "mutated-return"
	got[0].Env["K"] = "changed"
	again := m.Agents()
	if again[0].ID != "echo" {
		t.Fatalf("Agents() after mutating return = %q, want echo", again[0].ID)
	}
	if again[0].Env["K"] != "V" {
		t.Fatalf("Agents() env leaked mutation: %v", again[0].Env)
	}
}

func TestManager_NilCatalog(t *testing.T) {
	t.Parallel()

	m := NewManager(testACPConfig(true, 3), nil)
	if got := m.Agents(); got == nil || len(got) != 0 {
		t.Fatalf("Agents() = %v, want empty slice", got)
	}
	_, err := m.GetOrCreate(context.Background(), "echo", "/work")
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("error = %v, want ErrAgentNotFound", err)
	}
}

func TestManagerFromFiles_LoadsCatalog(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "acp_agents.json5")
	body := `{
  "agents": [
    { "id": "echo", "command": ["true"], "enabled": true }
  ]
}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	cfg := testACPConfig(true, 3)
	cfg.AgentsFile = path
	m, err := NewManagerFromFiles(cfg)
	if err != nil {
		t.Fatalf("NewManagerFromFiles: %v", err)
	}
	agents := m.Agents()
	if len(agents) != 1 || agents[0].ID != "echo" {
		t.Fatalf("Agents() = %+v, want [{id:echo}]", agents)
	}
}

func TestManagerFromFiles_EndToEnd(t *testing.T) {
	if _, err := os.Stat("session.go"); err != nil {
		t.Skip("leaf 03 session.go not available")
	}
	fakeSrc := filepath.Join("testdata", "fakeagent", "main.go")
	if _, err := os.Stat(fakeSrc); err != nil {
		t.Skip("leaf 03 fakeagent not available")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	build := execGoBuild(t, fakeSrc, bin)

	catalogPath := filepath.Join(dir, "acp_agents.json5")
	body := `{
  "agents": [
    { "id": "echo", "command": ["` + bin + `"], "enabled": true }
  ]
}
`
	if err := os.WriteFile(catalogPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	cfg := testACPConfig(true, 3)
	cfg.AgentsFile = catalogPath
	// Generous ceilings: handshake-under-parallel-load took >5s on a busy
	// box; these tests exercise logic, not timing. 30s still fails fast on
	// a genuine deadlock.
	cfg.DialTimeout = 30
	cfg.CallTimeout = 20
	m, err := NewManagerFromFiles(cfg)
	if err != nil {
		t.Fatalf("NewManagerFromFiles: %v", err)
	}
	if build == "" {
		t.Fatal("fakeagent build helper returned empty path")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sess, err := m.GetOrCreate(ctx, "echo", dir)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if _, err := sess.Send(ctx, "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	m.StopAll()
	if got := m.LiveSessions(); len(got) != 0 {
		t.Fatalf("LiveSessions after StopAll = %v, want empty", got)
	}
}

// execGoBuild is defined only so the e2e test compiles. The real helper
// lives below and is unused when the test skips.
func execGoBuild(t *testing.T, _, out string) string {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, "./testdata/fakeagent")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build fakeagent: %v\n%s", err, b)
	}
	return out
}

// ---------------------------------------------------------------------------
// Decision-lane wiring (acp-permission-decision leaf 02, Contract B)
// ---------------------------------------------------------------------------

// TestManager_SetDecisionClient_NilSafe pins the repo setter rule: every
// Set* tolerates a nil receiver and a nil value without panicking.
func TestManager_SetDecisionClient_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Manager
	m.SetDecisionClient(nil)                              // nil receiver: no panic
	m.SetDecisionClient(&stubDecisioner{available: true}) // nil receiver again

	m2 := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	m2.SetDecisionClient(nil) // clearing is legal
	m2.SetDecisionClient(&stubDecisioner{available: true})
}

// TestManager_HasDecisionClient pins the read-only inspection seam added
// for the daemon wiring tests (acp-permission-decision leaf 03): false on a
// fresh manager and on a nil receiver, true after SetDecisionClient, false
// again after clearing with nil.
func TestManager_HasDecisionClient(t *testing.T) {
	t.Parallel()

	var m *Manager
	if m.HasDecisionClient() {
		t.Fatal("nil receiver: HasDecisionClient() = true, want false (nil-safe)")
	}

	m2 := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	if m2.HasDecisionClient() {
		t.Fatal("fresh manager: HasDecisionClient() = true, want false")
	}
	m2.SetDecisionClient(&stubDecisioner{available: true})
	if !m2.HasDecisionClient() {
		t.Fatal("after SetDecisionClient: HasDecisionClient() = false, want true")
	}
	m2.SetDecisionClient(nil)
	if m2.HasDecisionClient() {
		t.Fatal("after clearing: HasDecisionClient() = true, want false")
	}
}

// TestManager_DecisionModeStampsSessionConfig pins that decision mode
// threads the injected Decisioner + cfg threshold into the SessionConfig
// handed to startFn.
func TestManager_DecisionModeStampsSessionConfig(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	acpCfg := testACPConfig(true, 3)
	acpCfg.PermissionMode = "decision"
	acpCfg.DecisionThreshold = 0.7
	m := NewManager(acpCfg, testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)
	dec := &stubDecisioner{available: true}
	m.SetDecisionClient(dec)

	if _, err := m.GetOrCreate(context.Background(), "echo", "/work"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.configs) != 1 {
		t.Fatalf("start configs = %d, want 1", len(rec.configs))
	}
	sc := rec.configs[0]
	if sc.Decision != Decisioner(dec) {
		t.Fatalf("SessionConfig.Decision = %v, want the injected decisioner", sc.Decision)
	}
	if sc.DecisionThreshold != 0.7 {
		t.Fatalf("SessionConfig.DecisionThreshold = %v, want 0.7 (from cfg)", sc.DecisionThreshold)
	}
}

// ---------------------------------------------------------------------------
// Human-relay wiring (acp-human-permission-relay leaf 02, Contract B)
// ---------------------------------------------------------------------------

// TestManager_SetPermissionRelay_NilSafe pins the repo setter rule for the
// single relay seam setter (drift 3): nil receiver tolerates any args, and
// a live manager tolerates nil relay / nil notifier (clearing is legal).
func TestManager_SetPermissionRelay_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Manager
	m.SetPermissionRelay(nil, nil)                    // nil receiver: no panic
	m.SetPermissionRelay(NewRelay(), &stubNotifier{}) // nil receiver again

	m2 := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	m2.SetPermissionRelay(nil, nil) // clearing is legal
	m2.SetPermissionRelay(NewRelay(), &stubNotifier{})
	if !m2.HasPermissionRelay() {
		t.Fatal("HasPermissionRelay after set = false, want true")
	}
	m2.SetPermissionRelay(nil, nil)
	if m2.HasPermissionRelay() {
		t.Fatal("HasPermissionRelay after clear = true, want false")
	}
	var nilRecv *Manager
	if nilRecv.HasPermissionRelay() {
		t.Fatal("nil receiver HasPermissionRelay = true, want false")
	}
}

// TestManager_PendingPermissions_NilSafe pins leaf 03's read surface:
// nil-safe empty on a nil manager and on a manager without a relay, and a
// passthrough snapshot once a relay is wired.
func TestManager_PendingPermissions_NilSafe(t *testing.T) {
	t.Parallel()

	var m *Manager
	if got := m.PendingPermissions(); got != nil {
		t.Fatalf("nil manager PendingPermissions = %v, want nil (nil-safe)", got)
	}

	m2 := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	if got := m2.PendingPermissions(); len(got) != 0 {
		t.Fatalf("no-relay PendingPermissions = %v, want empty", got)
	}

	rl := NewRelay()
	m2.SetPermissionRelay(rl, &stubNotifier{})
	if _, err := rl.Register(PendingPermission{ID: "perm-live", AgentID: "echo"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := m2.PendingPermissions()
	if len(got) != 1 || got[0].ID != "perm-live" {
		t.Fatalf("PendingPermissions = %+v, want [perm-live]", got)
	}
}

// TestManager_HumanModeStampsSessionConfig pins the human lane: relay,
// notifier, and RelayTimeout (seconds -> time.Duration, off cfg like
// DecisionThreshold) all flow into the SessionConfig handed to startFn.
func TestManager_HumanModeStampsSessionConfig(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	acpCfg := testACPConfig(true, 3)
	acpCfg.PermissionMode = "human"
	acpCfg.RelayTimeout = 45
	m := NewManager(acpCfg, testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)
	m.SetPermissionRelay(rl, notif)

	if _, err := m.GetOrCreate(context.Background(), "echo", "/work"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	sc := rec.configs[0]
	if sc.Relay != rl {
		t.Fatalf("SessionConfig.Relay = %v, want the injected relay", sc.Relay)
	}
	if sc.Notifier != notif {
		t.Fatalf("SessionConfig.Notifier = %v, want the injected notifier", sc.Notifier)
	}
	if sc.RelayTimeout != 45*time.Second {
		t.Fatalf("SessionConfig.RelayTimeout = %v, want 45s (cfg seconds -> Duration)", sc.RelayTimeout)
	}
}

// TestManager_DecisionEscalateStampsSessionConfig pins drift 3/master:
// decision mode + escalate=true stamps the relay lane (relay, notifier,
// timeout, DecisionEscalateToHuman) and KEEPS the decision seam — both
// lanes run in sequence.
func TestManager_DecisionEscalateStampsSessionConfig(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	rl := NewRelay()
	notif := &stubNotifier{listener: true}
	dec := &stubDecisioner{available: true}
	acpCfg := testACPConfig(true, 3)
	acpCfg.PermissionMode = "decision"
	acpCfg.DecisionThreshold = 0.7
	acpCfg.DecisionEscalateToHuman = true
	acpCfg.RelayTimeout = 30
	m := NewManager(acpCfg, testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)
	m.SetDecisionClient(dec)
	m.SetPermissionRelay(rl, notif)

	if _, err := m.GetOrCreate(context.Background(), "echo", "/work"); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	sc := rec.configs[0]
	if sc.Decision != Decisioner(dec) || sc.DecisionThreshold != 0.7 {
		t.Fatalf("decision seam lost under escalation: %+v", sc)
	}
	if sc.Relay != rl || sc.Notifier != PermissionNotifier(notif) {
		t.Fatalf("escalation stamp missing relay/notifier: %+v", sc)
	}
	if sc.RelayTimeout != 30*time.Second {
		t.Fatalf("SessionConfig.RelayTimeout = %v, want 30s", sc.RelayTimeout)
	}
	if !sc.DecisionEscalateToHuman {
		t.Fatal("SessionConfig.DecisionEscalateToHuman = false, want true")
	}
}

// TestManager_StampingMatrixNonHumanStaysUnstamped pins the byte-identical
// fail-closed default (drift 3 + master): permissive/deny/decision-without-
// escalate never receive relay fields even when a relay IS injected on the
// manager.
func TestManager_StampingMatrixNonHumanStaysUnstamped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*config.ACPConfig)
	}{
		{"permissive", func(c *config.ACPConfig) { c.PermissionMode = "permissive" }},
		{"deny", func(c *config.ACPConfig) { c.PermissionMode = "deny" }},
		{"decision_no_escalate", func(c *config.ACPConfig) {
			c.PermissionMode = "decision"
			c.DecisionEscalateToHuman = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &startRecorder{}
			acpCfg := testACPConfig(true, 3)
			tc.mutate(&acpCfg)
			acpCfg.RelayTimeout = 90
			m := NewManager(acpCfg, testAgents(
				config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
			))
			attachStart(m, rec)
			m.SetPermissionRelay(NewRelay(), &stubNotifier{})

			if _, err := m.GetOrCreate(context.Background(), "echo", "/work"); err != nil {
				t.Fatalf("GetOrCreate: %v", err)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			sc := rec.configs[0]
			if sc.Relay != nil {
				t.Fatalf("SessionConfig.Relay = %v, want nil in %s", sc.Relay, tc.name)
			}
			if sc.Notifier != nil {
				t.Fatalf("SessionConfig.Notifier = %v, want nil in %s", sc.Notifier, tc.name)
			}
			if sc.RelayTimeout != 0 {
				t.Fatalf("SessionConfig.RelayTimeout = %v, want 0 in %s", sc.RelayTimeout, tc.name)
			}
			if sc.DecisionEscalateToHuman {
				t.Fatalf("SessionConfig.DecisionEscalateToHuman = true in %s, want false", tc.name)
			}
		})
	}
}

// TestManager_NonDecisionModeLeavesLaneNil pins the byte-identical
// non-decision lane: permissive/deny never receive a Decisioner or
// threshold, even when one is injected on the manager.
func TestManager_NonDecisionModeLeavesLaneNil(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"permissive", "deny"} {
		t.Run(mode, func(t *testing.T) {
			rec := &startRecorder{}
			acpCfg := testACPConfig(true, 3)
			acpCfg.PermissionMode = mode
			acpCfg.DecisionThreshold = 0.5
			m := NewManager(acpCfg, testAgents(
				config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Enabled: true},
			))
			attachStart(m, rec)
			m.SetDecisionClient(&stubDecisioner{available: true})

			if _, err := m.GetOrCreate(context.Background(), "echo", "/work"); err != nil {
				t.Fatalf("GetOrCreate: %v", err)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			sc := rec.configs[0]
			if sc.Decision != nil {
				t.Fatalf("SessionConfig.Decision = %v, want nil in %s mode", sc.Decision, mode)
			}
			if sc.DecisionThreshold != 0 {
				t.Fatalf("SessionConfig.DecisionThreshold = %v, want 0 in %s mode", sc.DecisionThreshold, mode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Persona lane (acp-employees dim 2B, Multi-session Manager lane)
// ---------------------------------------------------------------------------

// TestManager_GetOrCreatePersona_DistinctSessions pins that two personas on
// one catalog agent get their own sessions (two startFn calls, distinct
// pointers) while the SessionConfig keeps the shared catalog identity.
func TestManager_GetOrCreatePersona_DistinctSessions(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 4), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"hermes", "acp"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	bob, err := m.GetOrCreatePersona(ctx, "hermes", "bob", "/work",
		PersonaOpts{CommandArgs: []string{"-p", "borg"}})
	if err != nil {
		t.Fatalf("GetOrCreatePersona bob: %v", err)
	}
	alice, err := m.GetOrCreatePersona(ctx, "hermes", "alice", "/work",
		PersonaOpts{CommandArgs: []string{"-p", "alice"}})
	if err != nil {
		t.Fatalf("GetOrCreatePersona alice: %v", err)
	}
	if bob == alice {
		t.Fatal("two personas returned the same session")
	}
	if rec.calls() != 2 {
		t.Fatalf("start called %d times, want 2", rec.calls())
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.configs[0].Persona != "bob" || rec.configs[1].Persona != "alice" {
		t.Fatalf("SessionConfig.Persona = %q/%q, want bob/alice",
			rec.configs[0].Persona, rec.configs[1].Persona)
	}
	for _, sc := range rec.configs {
		if sc.AgentID != "hermes" {
			t.Fatalf("SessionConfig.AgentID = %q, want hermes (catalog id)", sc.AgentID)
		}
		if len(sc.Command) != 2 || sc.Command[0] != "hermes" || sc.Command[1] != "acp" {
			t.Fatalf("SessionConfig.Command = %v, want the untouched catalog command", sc.Command)
		}
	}
	if got := rec.configs[0].CommandArgs; len(got) != 2 || got[0] != "-p" || got[1] != "borg" {
		t.Fatalf("bob CommandArgs = %v, want [-p borg]", got)
	}
	if got := rec.configs[1].CommandArgs; len(got) != 2 || got[0] != "-p" || got[1] != "alice" {
		t.Fatalf("alice CommandArgs = %v, want [-p alice]", got)
	}
	t.Cleanup(m.StopAll)
}

// TestManager_GetOrCreatePersona_SamePersonaDedups pins that the same
// persona requested twice returns the cached session, and that a concurrent
// second request dedups through the pending map (per-persona startWait).
func TestManager_GetOrCreatePersona_SamePersonaDedups(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{delay: 30 * time.Millisecond}
	m := NewManager(testACPConfig(true, 4), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"hermes", "acp"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	var first, second *Session
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		first, err1 = m.GetOrCreatePersona(ctx, "hermes", "bob", "/work", PersonaOpts{})
	}()
	go func() {
		defer wg.Done()
		second, err2 = m.GetOrCreatePersona(ctx, "hermes", "bob", "/work", PersonaOpts{})
	}()
	wg.Wait()
	if err1 != nil || err2 != nil {
		t.Fatalf("errors = %v/%v, want nil/nil", err1, err2)
	}
	if first != second {
		t.Fatal("concurrent same-persona requests returned different sessions")
	}
	if rec.calls() != 1 {
		t.Fatalf("start called %d times, want 1 (pending dedup per persona)", rec.calls())
	}
	// a cached hit does not re-start either
	third, err := m.GetOrCreatePersona(ctx, "hermes", "bob", "/work", PersonaOpts{})
	if err != nil {
		t.Fatalf("cached GetOrCreatePersona: %v", err)
	}
	if third != first {
		t.Fatal("cached GetOrCreatePersona returned a different session")
	}
	if rec.calls() != 1 {
		t.Fatalf("start called %d times after cached hit, want 1", rec.calls())
	}
	t.Cleanup(m.StopAll)
}

// TestManager_GetOrCreate_DelegatesNoPersona pins the back-compat lane:
// GetOrCreate stamps an empty persona, empty CommandArgs, and the catalog
// defaults — byte-identical to the pre-persona SessionConfig — and shares
// the map lane with GetOrCreatePersona(persona="").
func TestManager_GetOrCreate_DelegatesNoPersona(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 4), testAgents(
		config.ACPAgentEntry{ID: "echo", Command: []string{"agent"}, Env: map[string]string{"K": "V"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	viaLegacy, err := m.GetOrCreate(ctx, "echo", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	// persona "" through the new method hits the SAME key: no second start
	viaExplicit, err := m.GetOrCreatePersona(ctx, "echo", "", "/work", PersonaOpts{})
	if err != nil {
		t.Fatalf("GetOrCreatePersona empty persona: %v", err)
	}
	if viaLegacy != viaExplicit {
		t.Fatal("GetOrCreate and GetOrCreatePersona(\"\") returned different sessions")
	}
	if rec.calls() != 1 {
		t.Fatalf("start called %d times, want 1", rec.calls())
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	sc := rec.configs[0]
	if sc.Persona != "" {
		t.Fatalf("SessionConfig.Persona = %q, want empty (no-persona lane)", sc.Persona)
	}
	if sc.CommandArgs != nil {
		t.Fatalf("SessionConfig.CommandArgs = %v, want nil (byte-identical legacy shape)", sc.CommandArgs)
	}
	if sc.AgentID != "echo" || sc.Cwd != "/work" || sc.Env["K"] != "V" || sc.PermissionMode != "permissive" {
		t.Fatalf("legacy SessionConfig drifted: %+v", sc)
	}
	t.Cleanup(m.StopAll)
}

// TestManager_GetOrCreatePersona_MaxAgentsCountsPersonas pins that the cap
// now bounds total persona sessions: two personas on one agent exhaust
// MaxAgents=2 and a third hits ErrMaxAgents.
func TestManager_GetOrCreatePersona_MaxAgentsCountsPersonas(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 2), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"hermes", "acp"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	if _, err := m.GetOrCreatePersona(ctx, "hermes", "bob", "/work", PersonaOpts{}); err != nil {
		t.Fatalf("bob: %v", err)
	}
	if _, err := m.GetOrCreatePersona(ctx, "hermes", "alice", "/work", PersonaOpts{}); err != nil {
		t.Fatalf("alice: %v", err)
	}
	_, err := m.GetOrCreatePersona(ctx, "hermes", "carol", "/work", PersonaOpts{})
	if !errors.Is(err, ErrMaxAgents) {
		t.Fatalf("error = %v, want ErrMaxAgents (cap counts persona sessions)", err)
	}
	t.Cleanup(m.StopAll)
}

// TestManager_GetOrCreatePersona_CwdOverride pins the opts.Cwd precedence:
// an explicit Cwd beats both entry.Cwd and the workdir argument.
func TestManager_GetOrCreatePersona_CwdOverride(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 4), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Cwd: "/catalog/cwd", Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	if _, err := m.GetOrCreatePersona(ctx, "hermes", "bob", "/session/workdir",
		PersonaOpts{Cwd: "/persona/cwd"}); err != nil {
		t.Fatalf("GetOrCreatePersona: %v", err)
	}
	// empty opts.Cwd keeps the existing resolution (entry.Cwd wins)
	if _, err := m.GetOrCreatePersona(ctx, "hermes", "alice", "/session/workdir",
		PersonaOpts{}); err != nil {
		t.Fatalf("GetOrCreatePersona alice: %v", err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := rec.configs[0].Cwd; got != "/persona/cwd" {
		t.Fatalf("bob Cwd = %q, want /persona/cwd (opts override)", got)
	}
	if got := rec.configs[1].Cwd; got != "/catalog/cwd" {
		t.Fatalf("alice Cwd = %q, want /catalog/cwd (entry wins over workdir)", got)
	}
	t.Cleanup(m.StopAll)
}

// TestManager_GetOrCreatePersona_PermissionModeOverride pins that the opts
// override participates in the SAME conditional stamps: a human override on
// a permissive manager stamps the relay seam; a decision override stamps
// the decision seam + threshold; a permissive override on a human manager
// stamps NEITHER.
func TestManager_GetOrCreatePersona_PermissionModeOverride(t *testing.T) {
	t.Parallel()

	t.Run("human_override_stamps_relay", func(t *testing.T) {
		rec := &startRecorder{}
		rl := NewRelay()
		notif := &stubNotifier{listener: true}
		acpCfg := testACPConfig(true, 4)
		acpCfg.PermissionMode = "permissive"
		acpCfg.RelayTimeout = 42
		m := NewManager(acpCfg, testAgents(
			config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Enabled: true},
		))
		attachStart(m, rec)
		m.SetPermissionRelay(rl, notif)

		if _, err := m.GetOrCreatePersona(context.Background(), "hermes", "bob", "/work",
			PersonaOpts{PermissionMode: "human"}); err != nil {
			t.Fatalf("GetOrCreatePersona: %v", err)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		sc := rec.configs[0]
		if sc.PermissionMode != "human" {
			t.Fatalf("PermissionMode = %q, want human (override)", sc.PermissionMode)
		}
		if sc.Relay != rl || sc.Notifier != PermissionNotifier(notif) {
			t.Fatalf("human override did not stamp relay/notifier: %+v", sc)
		}
		if sc.RelayTimeout != 42*time.Second {
			t.Fatalf("RelayTimeout = %v, want 42s", sc.RelayTimeout)
		}
		if sc.Decision != nil {
			t.Fatalf("Decision = %v, want nil (override is human, not decision)", sc.Decision)
		}
		t.Cleanup(m.StopAll)
	})

	t.Run("decision_override_stamps_decision_seam", func(t *testing.T) {
		rec := &startRecorder{}
		dec := &stubDecisioner{available: true}
		acpCfg := testACPConfig(true, 4)
		acpCfg.PermissionMode = "permissive"
		acpCfg.DecisionThreshold = 0.4
		m := NewManager(acpCfg, testAgents(
			config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Enabled: true},
		))
		attachStart(m, rec)
		m.SetDecisionClient(dec)

		// opts threshold overrides the cfg value
		if _, err := m.GetOrCreatePersona(context.Background(), "hermes", "bob", "/work",
			PersonaOpts{PermissionMode: "decision", DecisionThreshold: 0.9}); err != nil {
			t.Fatalf("GetOrCreatePersona: %v", err)
		}
		// zero opts threshold falls back to cfg
		if _, err := m.GetOrCreatePersona(context.Background(), "hermes", "alice", "/work",
			PersonaOpts{PermissionMode: "decision"}); err != nil {
			t.Fatalf("GetOrCreatePersona alice: %v", err)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		bob := rec.configs[0]
		if bob.Decision != Decisioner(dec) {
			t.Fatalf("decision override did not stamp the seam: %+v", bob)
		}
		if bob.DecisionThreshold != 0.9 {
			t.Fatalf("bob DecisionThreshold = %v, want 0.9 (opts override)", bob.DecisionThreshold)
		}
		alice := rec.configs[1]
		if alice.DecisionThreshold != 0.4 {
			t.Fatalf("alice DecisionThreshold = %v, want 0.4 (cfg fallback)", alice.DecisionThreshold)
		}
		if bob.Relay != nil || alice.Relay != nil {
			t.Fatal("decision-without-escalate must not stamp the relay")
		}
		t.Cleanup(m.StopAll)
	})

	t.Run("permissive_override_stamps_nothing", func(t *testing.T) {
		rec := &startRecorder{}
		rl := NewRelay()
		notif := &stubNotifier{listener: true}
		acpCfg := testACPConfig(true, 4)
		acpCfg.PermissionMode = "human"
		acpCfg.RelayTimeout = 42
		m := NewManager(acpCfg, testAgents(
			config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Enabled: true},
		))
		attachStart(m, rec)
		m.SetPermissionRelay(rl, notif)
		m.SetDecisionClient(&stubDecisioner{available: true})

		if _, err := m.GetOrCreatePersona(context.Background(), "hermes", "bob", "/work",
			PersonaOpts{PermissionMode: "permissive"}); err != nil {
			t.Fatalf("GetOrCreatePersona: %v", err)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		sc := rec.configs[0]
		if sc.PermissionMode != "permissive" {
			t.Fatalf("PermissionMode = %q, want permissive (override)", sc.PermissionMode)
		}
		if sc.Relay != nil || sc.Notifier != nil || sc.RelayTimeout != 0 {
			t.Fatalf("permissive override leaked relay stamps: %+v", sc)
		}
		if sc.Decision != nil || sc.DecisionThreshold != 0 {
			t.Fatalf("permissive override leaked decision stamps: %+v", sc)
		}
		t.Cleanup(m.StopAll)
	})
}

// TestManager_GetOrCreatePersona_CommandArgsNeverMutatesCatalog pins that
// opts.CommandArgs are copied, not appended into the catalog entry's
// Command slice (aliasing guard).
func TestManager_GetOrCreatePersona_CommandArgsNeverMutatesCatalog(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	catalog := testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"hermes", "acp"}, Enabled: true},
	)
	m := NewManager(testACPConfig(true, 4), catalog)
	attachStart(m, rec)

	ctx := context.Background()
	if _, err := m.GetOrCreatePersona(ctx, "hermes", "bob", "/work",
		PersonaOpts{CommandArgs: []string{"-p", "borg"}}); err != nil {
		t.Fatalf("GetOrCreatePersona: %v", err)
	}
	rec.mu.Lock()
	sc := rec.configs[0]
	rec.mu.Unlock()
	if len(sc.Command) != 2 || sc.Command[0] != "hermes" || sc.Command[1] != "acp" {
		t.Fatalf("SessionConfig.Command = %v, want untouched [hermes acp]", sc.Command)
	}
	// mutating the stamped CommandArgs must not touch the catalog or the
	// SessionConfig.Command backing array
	sc.CommandArgs[0] = "mutated"
	rec.mu.Lock()
	fresh := rec.configs[0]
	rec.mu.Unlock()
	if got := m.Agents()[0].Command; len(got) != 2 || got[0] != "hermes" || got[1] != "acp" {
		t.Fatalf("catalog Command mutated: %v", got)
	}
	if fresh.Command[1] != "acp" {
		t.Fatalf("SessionConfig.Command aliased CommandArgs: %v", fresh.Command)
	}
	t.Cleanup(m.StopAll)
}

// TestManager_StopPersona pins the per-persona stop lane: StopPersona
// closes only the named persona's session; the sibling and the no-persona
// lane survive. LiveSessions labels persona sessions "agent/persona".
func TestManager_StopPersona(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	m := NewManager(testACPConfig(true, 4), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(m, rec)

	ctx := context.Background()
	plain, err := m.GetOrCreate(ctx, "hermes", "/work")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	bob, err := m.GetOrCreatePersona(ctx, "hermes", "bob", "/work", PersonaOpts{})
	if err != nil {
		t.Fatalf("GetOrCreatePersona bob: %v", err)
	}
	alice, err := m.GetOrCreatePersona(ctx, "hermes", "alice", "/work", PersonaOpts{})
	if err != nil {
		t.Fatalf("GetOrCreatePersona alice: %v", err)
	}

	live := m.LiveSessions()
	if len(live) != 3 {
		t.Fatalf("LiveSessions = %v, want 3 entries", live)
	}
	if live["hermes"] != plain.State() {
		t.Fatalf("LiveSessions[hermes] = %v, want the no-persona state", live["hermes"])
	}
	if _, ok := live["hermes/bob"]; !ok {
		t.Fatalf("LiveSessions missing hermes/bob: %v", live)
	}

	if err := m.StopPersona("hermes", "bob"); err != nil {
		t.Fatalf("StopPersona: %v", err)
	}
	if bob.State() != StateClosed {
		t.Fatalf("bob state = %v, want StateClosed", bob.State())
	}
	if alice.State() == StateClosed || plain.State() == StateClosed {
		t.Fatal("StopPersona closed a sibling session")
	}
	live = m.LiveSessions()
	if _, ok := live["hermes/bob"]; ok {
		t.Fatalf("LiveSessions still lists hermes/bob: %v", live)
	}
	// legacy Stop only touches the no-persona lane
	if err := m.Stop("hermes"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if plain.State() != StateClosed {
		t.Fatalf("plain state = %v, want StateClosed", plain.State())
	}
	if alice.State() == StateClosed {
		t.Fatal("legacy Stop closed a persona session")
	}
	// StopAll closes the rest; idempotent
	m.StopAll()
	if alice.State() != StateClosed {
		t.Fatalf("alice state = %v, want StateClosed after StopAll", alice.State())
	}
}

// TestManager_GetOrCreatePersona_DisabledAndNotFound pins that the persona
// lane honors the same gates as the legacy lane.
func TestManager_GetOrCreatePersona_DisabledAndNotFound(t *testing.T) {
	t.Parallel()

	rec := &startRecorder{}
	off := NewManager(testACPConfig(false, 3), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Enabled: true},
	))
	attachStart(off, rec)
	if _, err := off.GetOrCreatePersona(context.Background(), "hermes", "bob", "/work", PersonaOpts{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}

	on := NewManager(testACPConfig(true, 3), testAgents(
		config.ACPAgentEntry{ID: "hermes", Command: []string{"agent"}, Enabled: false},
	))
	attachStart(on, rec)
	if _, err := on.GetOrCreatePersona(context.Background(), "hermes", "bob", "/work", PersonaOpts{}); !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("error = %v, want ErrAgentDisabled", err)
	}
	if _, err := on.GetOrCreatePersona(context.Background(), "missing", "bob", "/work", PersonaOpts{}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("error = %v, want ErrAgentNotFound", err)
	}
	if rec.calls() != 0 {
		t.Fatalf("start called %d times, want 0 (gated paths must not spawn)", rec.calls())
	}
}
