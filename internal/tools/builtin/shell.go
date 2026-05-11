package builtin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/tools"
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
type ShellCommandRisk int

const (
	RiskMedium ShellCommandRisk = iota
	RiskHigh
	RiskCritical
)

func (r ShellCommandRisk) String() string {
	switch r {
	case RiskMedium:
		return "MEDIUM"
	case RiskHigh:
		return "HIGH"
	case RiskCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

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

// ShellExecuteTool executes shell commands in a sandboxed subprocess.
type ShellExecuteTool struct {
	workingDir         string
	defaultTimeout     time.Duration
	securityOrch       *intsecurity.Orchestrator
	knownSafeCommands  map[string]struct{}
}

// NewShellExecuteTool creates a new shell execution tool.
func NewShellExecuteTool(workingDir string, defaultTimeout time.Duration) *ShellExecuteTool {
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
	}
}

// SetSecurityOrchestrator sets the security orchestrator for command scanning.
func (t *ShellExecuteTool) SetSecurityOrchestrator(orch *intsecurity.Orchestrator) {
	t.securityOrch = orch
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

func (t *ShellExecuteTool) Name() string { return "shell" }

func (t *ShellExecuteTool) Description() string {
	return "Execute a shell command and return its stdout and stderr. Use for running system commands, scripts, and CLI tools. Commands run in a sandboxed subprocess with a timeout."
}

func (t *ShellExecuteTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"command": {
				Type:        "string",
				Description: "The shell command to execute.",
			},
			"timeout": {
				Type:        "number",
				Description: "Timeout in seconds (default 30).",
			},
			"working_dir": {
				Type:        "string",
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

	// Check command risk level (built-in check)
	risk := t.classifyRisk(command)
	if risk == RiskCritical {
		baseCmd := extractBaseCommand(command)
		return nil, fmt.Errorf("command blocked for safety: %s", baseCmd)
	}

	// Scan command with Tirith via security orchestrator (if configured)
	if t.securityOrch != nil {
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

	// Execute the command
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Get outputs
	stdoutStr := stdout.String()
	stderrStr := stderr.String()
	truncated := false

	// Truncate if too large
	if len(stdoutStr) > MaxOutputSize {
		stdoutStr = stdoutStr[:MaxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(stdout.String()))
		truncated = true
	}
	if len(stderrStr) > MaxOutputSize {
		stderrStr = stderrStr[:MaxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(stderr.String()))
		truncated = true
	}

	// Determine return code
	returnCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out after %v", timeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			returnCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
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
		Success:  returnCode == 0,
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
	if strings.Contains(command, "|") {
		segments := strings.Split(command, "|")
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

// extractBaseCommand extracts the base command from a shell command string.
func extractBaseCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	// Skip environment variable assignments (FOO=bar cmd ...)
	parts := strings.Fields(command)
	for _, part := range parts {
		if strings.Contains(part, "=") && !strings.HasPrefix(part, "-") {
			continue
		}
		// Return basename only
		return filepath.Base(part)
	}

	return ""
}

// Ensure ShellExecuteTool implements the Tool interface
var _ tools.Tool = (*ShellExecuteTool)(nil)
