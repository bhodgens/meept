package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
	Output       *RingBuffer
	CreatedAt    time.Time
	LastActivity time.Time

	// CurrentThreads caches the most recent thread list from the adapter.
	CurrentThreadID int
}

// Manager manages debug sessions.
type Manager struct {
	mu          sync.Mutex
	sessions    map[string]*DebugSession
	active      string // ID of the most recently active session
	nextID      atomic.Int64
	logger      *slog.Logger
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
		Output:       NewRingBuffer(1024 * 1024), // 1MB output buffer
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	// Register session.
	m.mu.Lock()
	m.sessions[id] = session
	m.active = id
	m.mu.Unlock()

	// Start the client read loop using a background context so it
	// persists beyond the caller's request context.
	bgCtx := context.Background()
	client.Start(bgCtx)

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

	session.State = SessionConfigured
	session.LastActivity = time.Now()

	m.logger.Info("debug session launched",
		"id", id,
		"adapter", adapterCfg.Name,
		"program", program,
	)

	return session, nil
}

// drainEvents reads events from the client and updates session state.
func (m *Manager) drainEvents(session *DebugSession) {
	for evt := range session.Client.Events() {
		session.LastActivity = time.Now()

		switch evt.Event {
		case "stopped":
			session.State = SessionStopped
			var body StoppedEventBody
			if err := parseJSON(evt.Body, &body); err == nil {
				session.CurrentThreadID = body.ThreadID
			}
		case "continued":
			session.State = SessionRunning
		case "terminated":
			session.State = SessionTerminated
		case "exited":
			session.State = SessionTerminated
		case "output":
			var body OutputEventBody
			if err := parseJSON(evt.Body, &body); err == nil {
				_, _ = session.Output.Write([]byte(body.Output))
			}
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

	session.State = SessionTerminated

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
