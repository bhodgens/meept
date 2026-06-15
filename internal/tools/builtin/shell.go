package builtin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/runtime"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/pty"
	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/security"
)

const (
	// DefaultShellTimeout is the default timeout for shell commands.
	DefaultShellTimeout = 30 * time.Second
	// MaxOutputSize is the maximum output size before truncation.
	MaxOutputSize = 50000
)

// ShellCommandRisk represents the risk level of a shell command.
//go:generate go run golang.org/x/tools/cmd/stringer -type=ShellCommandRisk
type ShellCommandRisk int

const (
	RiskMedium ShellCommandRisk = iota // MEDIUM
	RiskHigh                           // HIGH
	RiskCritical                       // CRITICAL
)

// readOnlyCommands are considered low-risk read operations.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "grep": true, "find": true,
	"wc": true, "du": true, "df": true, "file": true, "stat": true, "which": true,
	"whereis": true, "whoami": true, "hostname": true, "uname": true, "date": true,
	"uptime": true, "env": true, "printenv": true, "echo": true, "pwd": true,
	"id": true, "tree": true, "diff": true, "md5sum": true, "sha256sum": true,
	"shasum": true, "sort": true, "uniq": true, "tr": true, "cut": true, "awk": true,
	"sed": true, "rg": true, "fd": true, "bat": true, "less": true, "more": true,
	"realpath": true, "basename": true, "dirname": true, "ps": true, "top": true,
	"htop": true, "free": true, "lsof": true, "netstat": true, "ss": true,
	"git": true, "python3": true, "python": true, "pip": true, "npm": true,
	"node": true, "cargo": true, "rustc": true, "go": true, "java": true,
	"javac": true, "make": true, "cmake": true,
}

// blockedCommands are always denied.
var blockedCommands = map[string]bool{
	"rm": true, "rmdir": true, "mkfs": true, "dd": true, "fdisk": true, "parted": true,
	"shutdown": true, "reboot": true, "halt": true, "poweroff": true, "init": true,
	"iptables": true, "ip6tables": true, "nft": true,
	"passwd": true, "useradd": true, "userdel": true, "usermod": true, "groupadd": true,
	"chown": true, "chmod": true,
	"mount": true, "umount": true,
	"kill": true, "killall": true, "pkill": true,
}

// dangerousPattern matches high-risk command patterns.
var dangerousPattern = regexp.MustCompile(
	`(?i)\b(rm\s+-rf|mkfs|dd\s+if=|chmod\s+-R|chown\s+-R|shutdown|reboot` +
		`|init\s+[06]|systemctl\s+(stop|disable|mask)|kill\s+-9` +
		`|iptables|nft|deluser|userdel|groupdel)\b`,
)

// FenceChecker validates paths against fence boundaries.
type FenceChecker interface {
	CheckPath(path string, op string) error
	CheckCommand(cmd string, workDir string) error
}

// ShellExecuteTool executes shell commands in a sandboxed subprocess.
type ShellExecuteTool struct {
	workingDir        string
	defaultTimeout    time.Duration
	securityOrch      *intsecurity.Orchestrator
	knownSafeCommands map[string]struct{}
	containerMgr        *runtime.ContainerManager
	backend           runtime.ExecutionBackend
	logger            *slog.Logger
	ptyMgr            *pty.Manager
	fenceChecker      FenceChecker
}

// NewShellExecuteTool creates a new shell execution tool.
func NewShellExecuteTool(workingDir string, defaultTimeout time.Duration, ptyMgr *pty.Manager) *ShellExecuteTool {
	if workingDir == "" {
		workingDir, _ = resolvePath("~")
	}
	if defaultTimeout == 0 {
		defaultTimeout = DefaultShellTimeout
	}
	return &ShellExecuteTool{
		workingDir:        workingDir,
		defaultTimeout:    defaultTimeout,
		knownSafeCommands: make(map[string]struct{}),
		ptyMgr:            ptyMgr,
	}
}

// SetSecurityOrchestrator sets the security orchestrator for command scanning.
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *ShellExecuteTool) SetSecurityOrchestrator(orch *intsecurity.Orchestrator) {
	if orch != nil {
		t.securityOrch = orch
	}
}

// SetFenceChecker sets the fence checker for path-based sandboxing.
func (t *ShellExecuteTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

// SetRuntimeManager injects a runtime manager for backend-based execution.
// When set, commands are routed through the configured backend (local or docker).
// When nil, the tool falls back to direct exec.Command (original behavior) and
// any previously-injected manager is cleared.
func (t *ShellExecuteTool) SetRuntimeManager(mgr *runtime.ContainerManager) {
	if mgr == nil {
		t.containerMgr = nil
		t.backend = nil
		return
	}
	t.containerMgr = mgr
	t.backend = mgr.GetDefaultBackend()
	// Derive the component logger from the existing tool logger so that any
	// fields set by upstream wiring (request-id, agent-id, etc.) are
	// preserved. Fall back to slog.Default() only when t.logger is nil.
	base := t.logger
	if base == nil {
		base = slog.Default()
	}
	t.logger = base.With("component", "shell-tool")
}

// SetKnownSafeCommands configures a set of base command names that are
// treated as low-risk (RiskMedium) instead of the default RiskHigh for
// unknown commands. This is an escape hatch for deployments that want to
// whitelist project-specific tools (e.g. "mytool", "mycli").
func (t *ShellExecuteTool) SetKnownSafeCommands(cmds []string) {
	safe := make(map[string]struct{}, len(cmds))
	for _, c := range cmds {
		c = strings.TrimSpace(c)
		if c != "" {
			safe[c] = struct{}{}
		}
	}
	t.knownSafeCommands = safe
}

func (t *ShellExecuteTool) Name() string { return schemaJobTypeShell }

func (t *ShellExecuteTool) Category() string { return "shell" }

func (t *ShellExecuteTool) Description() string {
	return "Execute a shell command and return its stdout and stderr. Use for running system commands, scripts, and CLI tools. Commands run in a sandboxed subprocess with a timeout."
}

func (t *ShellExecuteTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropCommand: {
				Type:        schemaTypeString,
				Description: "The shell command to execute.",
			},
			"timeout": {
				Type:        schemaTypeNumber,
				Description: "Timeout in seconds (default 30).",
			},
			"working_dir": {
				Type:        schemaTypeString,
				Description: "Working directory for the command (optional).",
			},
		},
		Required: []string{"command"},
	}
}

// ShellResult contains the output of a shell command.
type ShellResult struct {
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	ReturnCode int    `json:"return_code"`
	Truncated  bool   `json:"truncated,omitempty"`
}

func (t *ShellExecuteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("empty command")
	}

	// Parse timeout
	timeout := t.defaultTimeout
	if timeoutSec, ok := args["timeout"].(float64); ok && timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}

	// Parse working directory
	workDir := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		resolved, err := resolvePath(wd)
		if err != nil {
			return nil, fmt.Errorf("invalid working directory: %w", err)
		}
		workDir = resolved
	}

	// Check fence boundaries
	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckCommand(command, workDir); err != nil {
			return nil, fmt.Errorf("fence: %w", err)
		}
	}

	// Check command risk level (built-in check)
	risk := t.classifyRisk(command)
	if risk == RiskCritical {
		baseCmd := extractBaseCommand(command)
		return nil, fmt.Errorf("command blocked for safety: %s", baseCmd)
	}

	// Scan command with Tirith via security orchestrator
	// Security orchestrator should be configured for production use
	if t.securityOrch == nil {
		// Security orchestrator not configured - scanning skipped
	} else {
		blocked, warning, reason := t.securityOrch.ScanShellCommand(ctx, command)
		if blocked {
			return nil, fmt.Errorf("command blocked by security scanner: %s", reason)
		}
		// Warnings are logged by the orchestrator, but we continue execution
		_ = warning
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Get the result via backend or direct exec
	var stdoutStr, stderrStr string
	var returnCode int
	var truncated bool
	var execErr error

	if t.backend != nil {
		// Route through runtime backend
		result, err := t.backend.Execute(ctx, runtime.Command{
			Cmd:     command,
			Dir:     workDir,
			Timeout: timeout,
		})
		if err != nil {
			cancel()
			return nil, fmt.Errorf("backend execution failed: %w", err)
		}
		// Backend returns combined output
		stdoutStr = result.Output
		returnCode = result.ExitCode
	} else {
		// Direct exec (original behavior)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir = workDir

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		execErr = cmd.Run()

		stdoutStr = stdout.String()
		stderrStr = stderr.String()

		if execErr != nil {
			if ctx.Err() == context.DeadlineExceeded {
				cancel()
				return nil, fmt.Errorf("command timed out after %v", timeout)
			}
			exitErr := &exec.ExitError{}
			if errors.As(execErr, &exitErr) {
				returnCode = exitErr.ExitCode()
			} else {
				cancel()
				return nil, fmt.Errorf("failed to execute command: %w", execErr)
			}
		}
	}

	// Truncate if too large
	if len(stdoutStr) > MaxOutputSize {
		stdoutStr = stdoutStr[:MaxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(stdoutStr))
		truncated = true
	}
	if len(stderrStr) > MaxOutputSize {
		stderrStr = stderrStr[:MaxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(stderrStr))
		truncated = true
	}

	// Build evidence: exit code and output hash
	evidence := make([]models.Evidence, 0, 2)
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceProcessExit,
		command,
		fmt.Sprintf("%d", returnCode),
		t.Name(),
	))

	// Hash output for compactness (full output is still returned in result)
	outputForHash := stdoutStr + stderrStr
	if len(outputForHash) > MaxOutputSize {
		outputForHash = outputForHash[:MaxOutputSize]
	}
	h := sha256.Sum256([]byte(outputForHash))
	outputHash := hex.EncodeToString(h[:])
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceShellOutput,
		command,
		outputHash,
		t.Name(),
	))

	return tools.ToolResult{
		Success: returnCode == 0,
		Result: ShellResult{
			Stdout:     stdoutStr,
			Stderr:     stderrStr,
			ReturnCode: returnCode,
			Truncated:  truncated,
		},
		Evidence: evidence,
	}, nil
}

// ExecuteStreaming implements tools.StreamingTool. It runs the shell command
// and emits progress updates as the command produces output.
func (t *ShellExecuteTool) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	command, _ := args["command"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("empty command")
	}

	// Parse timeout
	timeout := t.defaultTimeout
	if timeoutSec, ok := args["timeout"].(float64); ok && timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}

	// Parse working directory
	workDir := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		resolved, err := resolvePath(wd)
		if err != nil {
			return nil, fmt.Errorf("invalid working directory: %w", err)
		}
		workDir = resolved
	}

	// Check fence boundaries
	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckCommand(command, workDir); err != nil {
			return nil, fmt.Errorf("fence: %w", err)
		}
	}

	// Check command risk level
	risk := t.classifyRisk(command)
	if risk == RiskCritical {
		baseCmd := extractBaseCommand(command)
		return nil, fmt.Errorf("command blocked for safety: %s", baseCmd)
	}

	// Scan command with Tirith via security orchestrator
	if t.securityOrch == nil {
		// Security orchestrator not configured - scanning skipped
	} else {
		blocked, _, reason := t.securityOrch.ScanShellCommand(ctx, command)
		if blocked {
			return nil, fmt.Errorf("command blocked by security scanner: %s", reason)
		}
	}

	// Emit initial progress
	onUpdate(tools.ProgressUpdate{
		Message: fmt.Sprintf("running %s...", command),
		Percent: 10,
	})

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute the command
	var stdoutStr, stderrStr string
	var returnCode int
	var truncated bool
	var streamErr error

	if t.backend != nil {
		// Route through runtime backend
		onUpdate(tools.ProgressUpdate{
			Message: fmt.Sprintf("executing %s via %s backend...", extractBaseCommand(command), t.backend.Name()),
			Percent: 50,
		})

		result, err := t.backend.Execute(ctx, runtime.Command{
			Cmd:     command,
			Dir:     workDir,
			Timeout: timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("backend execution failed: %w", err)
		}
		// Backend returns combined output
		stdoutStr = result.Output
		returnCode = result.ExitCode
	} else {
		// Direct exec with streaming
		onUpdate(tools.ProgressUpdate{
			Message: fmt.Sprintf("executing %s...", extractBaseCommand(command)),
			Percent: 50,
		})

		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir = workDir

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		streamErr = cmd.Run()

		stdoutStr = stdout.String()
		stderrStr = stderr.String()

		if streamErr != nil {
			if ctx.Err() == context.DeadlineExceeded {
				onUpdate(tools.ProgressUpdate{
					Message: fmt.Sprintf("command timed out after %v", timeout),
					Percent: 100,
				})
				return nil, fmt.Errorf("command timed out after %v", timeout)
			}
			exitErr := &exec.ExitError{}
			if errors.As(streamErr, &exitErr) {
				returnCode = exitErr.ExitCode()
			} else {
				return nil, fmt.Errorf("failed to execute command: %w", streamErr)
			}
		}
	}

	// Truncate if too large
	if len(stdoutStr) > MaxOutputSize {
		stdoutStr = stdoutStr[:MaxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(stdoutStr))
		truncated = true
	}
	if len(stderrStr) > MaxOutputSize {
		stderrStr = stderrStr[:MaxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(stderrStr))
		truncated = true
	}

	// Emit completion progress with output summary
	outputSummary := ""
	if len(stdoutStr) > 100 {
		outputSummary = stdoutStr[:100] + "..."
	} else {
		outputSummary = stdoutStr
	}
	onUpdate(tools.ProgressUpdate{
		Message: fmt.Sprintf("completed (exit code %d)", returnCode),
		Percent: 100,
		PartialResult: func() json.RawMessage {
			data, _ := json.Marshal(map[string]string{"output_preview": outputSummary})
			return data
		}(),
	})

	// Build evidence
	evidence := make([]models.Evidence, 0, 2)
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceProcessExit,
		command,
		fmt.Sprintf("%d", returnCode),
		t.Name(),
	))
	outputForHash := stdoutStr + stderrStr
	if len(outputForHash) > MaxOutputSize {
		outputForHash = outputForHash[:MaxOutputSize]
	}
	h := sha256.Sum256([]byte(outputForHash))
	outputHash := hex.EncodeToString(h[:])
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceShellOutput,
		command,
		outputHash,
		t.Name(),
	))

	return tools.ToolResult{
		Success: returnCode == 0,
		Result: ShellResult{
			Stdout:     stdoutStr,
			Stderr:     stderrStr,
			ReturnCode: returnCode,
			Truncated:  truncated,
		},
		Evidence: evidence,
	}, nil
}

// classifyRisk determines the risk level of a command.
func (t *ShellExecuteTool) classifyRisk(command string) ShellCommandRisk {
	command = strings.TrimSpace(command)
	if command == "" {
		return RiskMedium
	}

	baseCmd := extractBaseCommand(command)

	// Check blocked commands
	if blockedCommands[baseCmd] {
		return RiskCritical
	}

	// Check for sudo
	if baseCmd == "sudo" {
		return RiskCritical
	}

	// Check pipes first - evaluate each segment independently so that a
	// blocked/sudo segment in a pipeline is detected as CRITICAL rather than
	// being masked by a HIGH dangerous-pattern match on the full line.
	// Split on unquoted `|` only so commands like `awk -F'|' '{print $2}'`
	// are not broken apart at pipes inside quotes.
	if segments, ok := splitOnUnquotedPipes(command); ok {
		maxRisk := RiskMedium
		for _, seg := range segments {
			segRisk := t.classifyRisk(strings.TrimSpace(seg))
			if segRisk > maxRisk {
				maxRisk = segRisk
			}
		}
		return maxRisk
	}

	// Check dangerous patterns
	if dangerousPattern.MatchString(command) {
		return RiskHigh
	}

	// Check read-only commands
	if readOnlyCommands[baseCmd] {
		return RiskMedium
	}

	// Check operator-configured allowlist for otherwise-unknown commands.
	if _, ok := t.knownSafeCommands[baseCmd]; ok {
		return RiskMedium
	}

	// Default: HIGH for unknown commands
	return RiskHigh
}

// GetRiskLevel returns the risk level for a command (public accessor).
func (t *ShellExecuteTool) GetRiskLevel(command string) security.RiskLevel {
	risk := t.classifyRisk(command)
	switch risk {
	case RiskMedium:
		return security.RiskMedium
	case RiskHigh:
		return security.RiskHigh
	case RiskCritical:
		return security.RiskCritical
	default:
		return security.RiskMedium
	}
}

// PTYTool methods -------------------------------------------------------------

// CreateSession creates a new PTY session.
func (t *ShellExecuteTool) CreateSession(sessionID string, config tools.PTYSessionConfig) (*tools.PTYSessionInfo, error) {
	// SEC-H4 FIX: Check fence boundaries before creating PTY sessions.
	// Regular shell execution checks at lines 208-213, but PTY sessions
	// previously bypassed the fence entirely.
	if t.fenceChecker != nil && config.Dir != "" {
		if err := t.fenceChecker.CheckCommand(config.Cmd, config.Dir); err != nil {
			return nil, fmt.Errorf("pty session rejected by fence: %w", err)
		}
	}

	if t.ptyMgr == nil {
		return nil, fmt.Errorf("PTY manager not available")
	}

	ptyCfg := pty.SessionConfig{
		Cmd:  config.Cmd,
		Args: config.Args,
		Dir:  config.Dir,
		Rows: config.Rows,
		Cols: config.Cols,
	}
	if ptyCfg.Rows <= 0 {
		ptyCfg.Rows = 24
	}
	if ptyCfg.Cols <= 0 {
		ptyCfg.Cols = 80
	}

	sess, err := t.ptyMgr.CreateSession(sessionID, ptyCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create PTY session: %w", err)
	}

	return &tools.PTYSessionInfo{
		ID:        sessionID,
		Cmd:       config.Cmd,
		Args:      config.Args,
		Dir:       config.Dir,
		CreatedAt: time.Now(),
		Rows:      ptyCfg.Rows,
		Cols:      ptyCfg.Cols,
		IsRunning: sess.IsRunning(),
	}, nil
}

// WriteToSession sends input to a PTY session.
func (t *ShellExecuteTool) WriteToSession(sessionID string, input []byte) error {
	if t.ptyMgr == nil {
		return fmt.Errorf("PTY manager not available")
	}

	sess := t.ptyMgr.GetSession(sessionID)
	if sess == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	_, err := sess.Write(input)
	return err
}

// ReadFromSession reads output from a PTY session (context-aware).
func (t *ShellExecuteTool) ReadFromSession(ctx context.Context, sessionID string) ([]byte, error) {
	if t.ptyMgr == nil {
		return nil, fmt.Errorf("PTY manager not available")
	}

	sess := t.ptyMgr.GetSession(sessionID)
	if sess == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	buf := make([]byte, 4096)
	n, err := sess.Read(ctx, buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// CloseSession terminates a PTY session.
func (t *ShellExecuteTool) CloseSession(sessionID string) error {
	if t.ptyMgr == nil {
		return fmt.Errorf("PTY manager not available")
	}
	return t.ptyMgr.DestroySession(sessionID)
}

// SessionOutput returns a channel for streaming session output.
func (t *ShellExecuteTool) SessionOutput(sessionID string) (<-chan []byte, error) {
	if t.ptyMgr == nil {
		return nil, fmt.Errorf("PTY manager not available")
	}

	sess := t.ptyMgr.GetSession(sessionID)
	if sess == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return sess.Output(), nil
}

// Ensure ShellExecuteTool implements the Tool and StreamingTool interfaces
var (
	_ tools.Tool          = (*ShellExecuteTool)(nil)
	_ tools.StreamingTool = (*ShellExecuteTool)(nil)
	_ tools.PTYTool       = (*ShellExecuteTool)(nil)
)
