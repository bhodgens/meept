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
