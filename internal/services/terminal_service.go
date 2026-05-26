package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

// CommandHistory represents a shell command execution record.
type CommandHistory struct {
	ID         string             `json:"id"`
	Command    string             `json:"command"`
	Output     string             `json:"output,omitempty"`
	Stderr     string             `json:"stderr,omitempty"`
	ExitCode   int                `json:"exit_code"`
	Timestamp  time.Time          `json:"timestamp"`
	WorkingDir string             `json:"working_dir"`
	Duration   time.Duration      `json:"duration_ms"`
	RiskLevel  security.RiskLevel `json:"risk_level"`
	Success    bool               `json:"success"`
}

// TerminalService provides shell terminal functionality.
type TerminalService struct {
	shellTool    *builtin.ShellExecuteTool
	bus          *bus.MessageBus
	logger       *slog.Logger
	history      []CommandHistory
	historyMu    sync.RWMutex
	maxHistory   int
	workingDir   string
	sessionStore map[string]*TerminalSession
	sessionMu    sync.RWMutex
}

// TerminalSession represents an active terminal session.
type TerminalSession struct {
	ID           string
	WorkingDir   string
	CreatedAt    time.Time
	LastUsed     time.Time
	CommandCount int
}

// NewTerminalService creates a new terminal service.
func NewTerminalService(workingDir string, bus *bus.MessageBus, logger *slog.Logger) *TerminalService {
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	if logger == nil {
		logger = slog.Default()
	}

	shellTool := builtin.NewShellExecuteTool(workingDir, builtin.DefaultShellTimeout)

	return &TerminalService{
		shellTool:    shellTool,
		bus:          bus,
		logger:       logger,
		history:      make([]CommandHistory, 0),
		maxHistory:   100,
		workingDir:   workingDir,
		sessionStore: make(map[string]*TerminalSession),
	}
}

// SetKnownSafeCommands configures additional commands treated as low-risk.
func (svc *TerminalService) SetKnownSafeCommands(cmds []string) {
	if svc.shellTool != nil {
		svc.shellTool.SetKnownSafeCommands(cmds)
	}
}

// ExecuteCommand runs a shell command and records it in history.
func (svc *TerminalService) ExecuteCommand(ctx context.Context, cmd, workDir string) (*CommandHistory, error) {
	if strings.TrimSpace(cmd) == "" {
		return nil, ErrInvalidInput
	}

	startTime := time.Now()

	// Prepare arguments for shell tool
	args := map[string]any{
		"command": cmd,
	}
	if workDir != "" {
		args["working_dir"] = workDir
	}

	// Execute via shell tool
	result, err := svc.shellTool.Execute(ctx, args)
	duration := time.Since(startTime)

	// Determine success and extracts
	success := err == nil
	output := ""
	stderr := ""
	exitCode := 0

	if result != nil {
		if tr, ok := result.(builtin.ShellResult); ok {
			output = tr.Stdout
			stderr = tr.Stderr
			exitCode = tr.ReturnCode
			success = tr.ReturnCode == 0
		} else if tr, ok := result.(map[string]any); ok {
			if out, ok := tr["stdout"].(string); ok {
				output = out
			}
			if out, ok := tr["stderr"].(string); ok {
				stderr = out
			}
			if code, ok := tr["return_code"].(float64); ok {
				exitCode = int(code)
				success = code == 0
			}
		}
	}

	// Get risk level
	riskLevel := svc.shellTool.GetRiskLevel(cmd)

	// Create history entry
	historyEntry := CommandHistory{
		ID:         fmt.Sprintf("cmd-%d", startTime.UnixNano()),
		Command:    cmd,
		Output:     output,
		Stderr:     stderr,
		ExitCode:   exitCode,
		Timestamp:  startTime,
		WorkingDir: workDir,
		Duration:   duration,
		RiskLevel:  riskLevel,
		Success:    success,
	}

	// Add to history
	svc.historyMu.Lock()
	svc.history = append(svc.history, historyEntry)
	// Trim history if too long
	if len(svc.history) > svc.maxHistory {
		svc.history = svc.history[len(svc.history)-svc.maxHistory:]
	}
	svc.historyMu.Unlock()

	// Update or create session
	svc.updateSession(workDir)

	// Emit bus event
	if svc.bus != nil {
		msg, _ := models.NewBusMessage("terminal.command", "terminal", map[string]any{
			"id":      historyEntry.ID,
			"command": cmd,
			"success": success,
		})
		if msg != nil {
			svc.bus.Publish("terminal.command", msg)
		}
	}

	if err != nil {
		return &historyEntry, err
	}

	return &historyEntry, nil
}

// GetHistory returns recent command history.
func (svc *TerminalService) GetHistory(limit int) []CommandHistory {
	svc.historyMu.RLock()
	defer svc.historyMu.RUnlock()

	if limit <= 0 || limit > len(svc.history) {
		limit = len(svc.history)
	}

	// Return most recent first
	result := make([]CommandHistory, limit)
	for i := 0; i < limit; i++ {
		result[i] = svc.history[len(svc.history)-1-i]
	}
	return result
}

// GetSession returns a terminal session by ID.
func (svc *TerminalService) GetSession(id string) (*TerminalSession, error) {
	svc.sessionMu.RLock()
	defer svc.sessionMu.RUnlock()

	session, ok := svc.sessionStore[id]
	if !ok {
		return nil, ErrNotFound
	}
	return session, nil
}

// ListSessions returns all active terminal sessions.
func (svc *TerminalService) ListSessions() []*TerminalSession {
	svc.sessionMu.RLock()
	defer svc.sessionMu.RUnlock()

	sessions := make([]*TerminalSession, 0, len(svc.sessionStore))
	for _, s := range svc.sessionStore {
		sessions = append(sessions, s)
	}

	// Sort by last used (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsed.After(sessions[j].LastUsed)
	})

	return sessions
}

// updateSession updates or creates a session for the working directory.
func (svc *TerminalService) updateSession(workDir string) {
	if workDir == "" {
		workDir = svc.workingDir
	}

	// Normalize working directory
	absPath, err := filepath.Abs(workDir)
	if err != nil {
		absPath = workDir
	}

	svc.sessionMu.Lock()
	defer svc.sessionMu.Unlock()

	// Find or create session for this working directory
	var session *TerminalSession
	for _, s := range svc.sessionStore {
		if s.WorkingDir == absPath {
			session = s
			break
		}
	}

	now := time.Now()
	if session == nil {
		session = &TerminalSession{
			ID:           fmt.Sprintf("session-%s", absPath),
			WorkingDir:   absPath,
			CreatedAt:    now,
			LastUsed:     now,
			CommandCount: 1,
		}
		svc.sessionStore[session.ID] = session
	} else {
		session.LastUsed = now
		session.CommandCount++
	}
}

// ClearHistory removes all command history.
func (svc *TerminalService) ClearHistory() {
	svc.historyMu.Lock()
	defer svc.historyMu.Unlock()
	svc.history = make([]CommandHistory, 0)
}

// SetMaxHistory sets the maximum history size.
func (svc *TerminalService) SetMaxHistory(max int) {
	svc.historyMu.Lock()
	defer svc.historyMu.Unlock()
	svc.maxHistory = max
	if len(svc.history) > max {
		svc.history = svc.history[len(svc.history)-max:]
	}
}

// GetShellOutput implements JSON marshaling for shell results.
func GetShellOutput(result any) (stdout, stderr string, exitCode int, ok bool) {
	if result == nil {
		return "", "", 0, false
	}

	switch r := result.(type) {
	case builtin.ShellResult:
		return r.Stdout, r.Stderr, r.ReturnCode, true
	case map[string]any:
		stdout, _ = r["stdout"].(string)
		stderr, _ = r["stderr"].(string)
		if code, ok := r["return_code"].(float64); ok {
			exitCode = int(code)
		}
		return stdout, stderr, exitCode, true
	}

	// Try JSON marshaling as fallback
	data, err := json.Marshal(result)
	if err != nil {
		return "", "", 0, false
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return "", "", 0, false
	}

	stdout, _ = m["stdout"].(string)
	stderr, _ = m["stderr"].(string)
	if code, ok := m["return_code"].(float64); ok {
		exitCode = int(code)
	}
	return stdout, stderr, exitCode, true
}
