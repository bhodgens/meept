package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SessionState represents the state of a debug session.
type SessionState string

const (
	// SessionLaunching indicates the adapter is being started.
	SessionLaunching SessionState = "launching"
	// SessionConfigured indicates initialize + launch succeeded.
	SessionConfigured SessionState = "configured"
	// SessionRunning indicates the debug target is executing.
	SessionRunning SessionState = "running"
	// SessionStopped indicates the debug target is paused (e.g. at a breakpoint).
	SessionStopped SessionState = "stopped"
	// SessionTerminated indicates the debug target has exited.
	SessionTerminated SessionState = "terminated"
)

// DebugSession holds state for an active debug session.
type DebugSession struct {
	ID           string
	Adapter      string
	Client       *Client
	State        SessionState
	Mode         SessionMode
	Output       *RingBuffer
	CreatedAt    time.Time
	LastActivity time.Time
	Program      string // Program path or process identifier

	// CurrentThreads caches the most recent thread list from the adapter.
	CurrentThreadID int

	// mu protects State, LastActivity, CurrentThreadID from concurrent
	// access between drainEvents and readers (Manager.List/Active/Get
	// callers). Use RLock when reading these fields.
	mu sync.RWMutex

	// drainDone is closed when drainEvents exits, allowing Terminate to
	// wait for the goroutine to fully stop before marking the session
	// terminated (S6-3).
	drainDone chan struct{}
}

// Manager manages debug sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*DebugSession
	active   string // ID of the most recently active session
	nextID   atomic.Int64
	logger   *slog.Logger
}

// NewManager creates a new debug session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*DebugSession),
		logger:   slog.Default().With("component", "debug-manager"),
	}
}

// Launch creates a new debug session by spawning a DAP adapter, initializing
// it, and launching the target program.
func (m *Manager) Launch(ctx context.Context, adapterCfg *AdapterConfig, program string, args []string, cwd string) (*DebugSession, error) {
	if adapterCfg == nil {
		return nil, fmt.Errorf("adapter config is required")
	}
	if program == "" {
		return nil, fmt.Errorf("program path is required")
	}

	// Validate the adapter command to prevent command injection.
	if err := validateAdapterCommand(adapterCfg.Command); err != nil {
		return nil, fmt.Errorf("invalid adapter command: %w", err)
	}

	// Resolve working directory.
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Build command.
	cmdArgs := make([]string, len(adapterCfg.Args))
	copy(cmdArgs, adapterCfg.Args)
	cmd := exec.CommandContext(ctx, adapterCfg.Command, cmdArgs...)
	cmd.Dir = cwd

	client, err := NewClient(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to create DAP client: %w", err)
	}

	// Generate session ID.
	id := fmt.Sprintf("dbg-%d", m.nextID.Add(1))

	session := &DebugSession{
		ID:           id,
		Adapter:      adapterCfg.Name,
		Client:       client,
		State:        SessionLaunching,
		Mode:         SessionModeLaunch,
		Output:       NewRingBuffer(1024 * 1024), // 1MB output buffer
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Program:      program,
		drainDone:    make(chan struct{}),
	}

	// Register session.
	m.mu.Lock()
	m.sessions[id] = session
	m.active = id
	m.mu.Unlock()

	// Start the client read loop using a background context so it
	// persists beyond the caller's request context. The client's Close()
	// method kills the subprocess (unblocking reads) and cancels its
	// internal context.
	client.Start(context.Background())

	// Forward output events to the ring buffer.
	go m.drainEvents(session)

	// Initialize the adapter.
	if err := client.Initialize(ctx, adapterCfg.Name); err != nil {
		_ = client.Close()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to initialize adapter: %w", err)
	}

	// Launch the program.
	launchArgs := LaunchRequestArguments{
		Program: program,
		Args:    args,
		Cwd:     cwd,
	}
	if err := client.Launch(ctx, launchArgs); err != nil {
		_ = client.Close()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to launch program: %w", err)
	}

	session.mu.Lock()
	session.State = SessionConfigured
	session.LastActivity = time.Now()
	session.mu.Unlock()

	m.logger.Info("debug session launched",
		"id", id,
		"adapter", adapterCfg.Name,
		"program", program,
	)

	return session, nil
}

// Attach creates a new debug session by spawning a DAP adapter, initializing
// it, and attaching to a running process.
func (m *Manager) Attach(ctx context.Context, adapterCfg *AdapterConfig, pid int, processName string) (*DebugSession, error) {
	if adapterCfg == nil {
		return nil, fmt.Errorf("adapter config is required")
	}
	if pid <= 0 && processName == "" {
		return nil, fmt.Errorf("either processId or processName is required")
	}

	// Validate the adapter command to prevent command injection.
	if err := validateAdapterCommand(adapterCfg.Command); err != nil {
		return nil, fmt.Errorf("invalid adapter command: %w", err)
	}

	// Resolve PID from process name if needed.
	if pid <= 0 && processName != "" {
		resolvedPID, err := FindPIDByName(processName)
		if err != nil {
			return nil, fmt.Errorf("failed to find process %q: %w", processName, err)
		}
		pid = resolvedPID
	}

	// Build command.
	cmdArgs := make([]string, len(adapterCfg.Args))
	copy(cmdArgs, adapterCfg.Args)
	cmd := exec.CommandContext(ctx, adapterCfg.Command, cmdArgs...)

	client, err := NewClient(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to create DAP client: %w", err)
	}

	// Generate session ID.
	id := fmt.Sprintf("dbg-%d", m.nextID.Add(1))

	programLabel := processName
	if programLabel == "" {
		programLabel = fmt.Sprintf("pid:%d", pid)
	}

	session := &DebugSession{
		ID:           id,
		Adapter:      adapterCfg.Name,
		Client:       client,
		State:        SessionLaunching,
		Mode:         SessionModeAttach,
		Output:       NewRingBuffer(1024 * 1024), // 1MB output buffer
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Program:      programLabel,
		drainDone:    make(chan struct{}),
	}

	// Register session.
	m.mu.Lock()
	m.sessions[id] = session
	m.active = id
	m.mu.Unlock()

	// Start the client read loop using a background context so it
	// persists beyond the caller's request context. The client's Close()
	// method kills the subprocess (unblocking reads) and cancels its
	// internal context.
	client.Start(context.Background())

	// Forward output events to the ring buffer.
	go m.drainEvents(session)

	// Initialize the adapter.
	if err := client.Initialize(ctx, adapterCfg.Name); err != nil {
		_ = client.Close()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to initialize adapter: %w", err)
	}

	// Attach to the process.
	attachArgs := AttachRequestArguments{
		ProcessID: &pid,
	}
	if err := client.Attach(ctx, attachArgs); err != nil {
		_ = client.Close()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to attach to process %d: %w", pid, err)
	}

	session.mu.Lock()
	session.State = SessionConfigured
	session.LastActivity = time.Now()
	session.mu.Unlock()

	m.logger.Info("debug session attached",
		"id", id,
		"adapter", adapterCfg.Name,
		"pid", pid,
		"process_name", processName,
	)

	return session, nil
}

// LoadCore creates a new debug session by analyzing a core dump. Unlike launch
// and attach, this does not spawn a long-running DAP adapter. Instead it runs
// the native debugger in batch mode, parses the output, and returns a
// CoreDumpResult. The session is marked as terminated immediately since core
// analysis is a one-shot operation.
func (m *Manager) LoadCore(ctx context.Context, coreFile, program, adapterName string) (*CoreDumpResult, *DebugSession, error) {
	if coreFile == "" {
		return nil, nil, fmt.Errorf("core file path is required")
	}
	if program == "" {
		return nil, nil, fmt.Errorf("program path is required")
	}

	result, err := AnalyzeCoreDump(ctx, coreFile, program, adapterName)
	if err != nil {
		return nil, nil, err
	}

	// Create a terminated session for tracking purposes.
	id := fmt.Sprintf("dbg-%d", m.nextID.Add(1))

	session := &DebugSession{
		ID:           id,
		Adapter:      string(result.Adapter),
		State:        SessionTerminated,
		Mode:         SessionModeCore,
		Output:       NewRingBuffer(1024 * 1024),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Program:      fmt.Sprintf("%s (core: %s)", program, coreFile),
	}

	// Store the crash report in the output buffer.
	report := CrashReport(result)
	_, _ = session.Output.Write([]byte(report))

	// Register session.
	m.mu.Lock()
	m.sessions[id] = session
	m.active = id
	m.mu.Unlock()

	m.logger.Info("core dump analysis complete",
		"id", id,
		"adapter", result.Adapter,
		"program", program,
		"core_file", coreFile,
		"signal", result.Signal,
		"threads", len(result.Threads),
	)

	return result, session, nil
}

// drainEvents reads events from the client and updates session state.
func (m *Manager) drainEvents(session *DebugSession) {
	// Signal exit when the loop terminates so Terminate can synchronize.
	defer func() {
		if session.drainDone != nil {
			close(session.drainDone)
		}
	}()
	for evt := range session.Client.Events() {
		session.mu.Lock()
		session.LastActivity = time.Now()

		switch evt.Event {
		case "stopped":
			session.State = SessionStopped
			session.mu.Unlock()
			var body StoppedEventBody
			if err := parseJSON(evt.Body, &body); err == nil {
				session.mu.Lock()
				session.CurrentThreadID = body.ThreadID
				session.mu.Unlock()
			}
		case "continued":
			session.State = SessionRunning
			session.mu.Unlock()
		case "terminated":
			session.State = SessionTerminated
			session.mu.Unlock()
		case "exited":
			session.State = SessionTerminated
			session.mu.Unlock()
		case "output":
			session.mu.Unlock()
			var body OutputEventBody
			if err := parseJSON(evt.Body, &body); err == nil {
				_, _ = session.Output.Write([]byte(body.Output))
			}
		default:
			session.mu.Unlock()
		}

		m.logger.Debug("DAP event",
			"session", session.ID,
			"event", evt.Event,
		)
	}
}

// Active returns the currently active debug session, or nil.
func (m *Manager) Active() *DebugSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active == "" {
		return nil
	}
	return m.sessions[m.active]
}

// Get returns a debug session by ID.
func (m *Manager) Get(id string) (*DebugSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	return s, ok
}

// SetActive marks a session as the active one.
func (m *Manager) SetActive(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sessions[id]; !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	m.active = id
	return nil
}

// Terminate shuts down a specific debug session.
func (m *Manager) Terminate(ctx context.Context, id string) error {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session not found: %s", id)
	}
	delete(m.sessions, id)
	if m.active == id {
		m.active = ""
	}
	m.mu.Unlock()

	// Best-effort disconnect and terminate.
	disconnectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_ = session.Client.Disconnect(disconnectCtx)
	_ = session.Client.Close()

	// Wait for drainEvents to exit before marking state, to avoid a
	// race where drainEvents overwrites State after Terminate sets it
	// (S6-3). Use a timeout to avoid blocking forever if drainEvents
	// is stuck.
	if session.drainDone != nil {
		select {
		case <-session.drainDone:
		case <-time.After(2 * time.Second):
			m.logger.Warn("drainEvents did not exit within timeout", "session", id)
		}
	}

	session.mu.Lock()
	session.State = SessionTerminated
	session.mu.Unlock()

	m.logger.Info("debug session terminated", "id", id)
	return nil
}

// List returns all active sessions.
func (m *Manager) List() []*DebugSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := make([]*DebugSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// Close terminates all debug sessions.
func (m *Manager) Close() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var lastErr error
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, id := range ids {
		if err := m.Terminate(ctx, id); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// parseJSON is a small helper to unmarshal JSON.
func parseJSON(data []byte, v any) error {
	if len(data) == 0 {
		return fmt.Errorf("empty data")
	}
	return json.Unmarshal(data, v)
}

// validateAdapterCommand ensures the adapter command is a safe absolute path
// that exists on disk and does not contain path traversal sequences.
func validateAdapterCommand(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("command is empty")
	}
	if strings.Contains(cmd, "..") {
		return fmt.Errorf("command must not contain '..': %q", cmd)
	}
	if !filepath.IsAbs(cmd) {
		// Allow bare names that can be resolved via PATH (e.g. "dlv", "gdb").
		// Reject anything with path separators to prevent relative-path injection.
		if strings.ContainsAny(cmd, "/\\") {
			return fmt.Errorf("relative command path not allowed: %q", cmd)
		}
		// Verify the binary exists in PATH.
		if _, err := exec.LookPath(cmd); err != nil {
			return fmt.Errorf("command not found in PATH: %q", cmd)
		}
		return nil
	}
	// Absolute path: verify it exists on disk.
	info, err := os.Stat(cmd)
	if err != nil {
		return fmt.Errorf("command not found: %q", cmd)
	}
	if info.IsDir() {
		return fmt.Errorf("command path is a directory: %q", cmd)
	}
	return nil
}
