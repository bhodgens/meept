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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/pty"
	"github.com/caimlas/meept/internal/runtime"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/taint"
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
//
//go:generate go run golang.org/x/tools/cmd/stringer -type=ShellCommandRisk
type ShellCommandRisk int

const (
	RiskMedium   ShellCommandRisk = iota // MEDIUM
	RiskHigh                             // HIGH
	RiskCritical                         // CRITICAL
)

// readOnlyCommands are considered low-risk read operations.
//
// Note: interpreters (python/node/java/...), package managers (pip/npm/cargo),
// compilers (rustc/javac/cmake), and build runners (make/go) are intentionally
// excluded — they can execute arbitrary code, write files, or run lifecycle
// hooks. Treat them as RiskModerate or higher so callers gate them.
//
// SECURITY FIX (2026-07-14): git was removed from this list. Git can modify
// state (commit, push, rebase, filter-branch), read any file, and execute
// hooks. Commands like `git push`, `git commit --amend`, `git filter-branch`
// can have destructive effects. Git commands are now classified as RiskHigh
// (unknown command) and require explicit approval.
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

// credentialPathPattern matches shell commands that name the daemon's
// credential material (audit M5 Phase 2c). Best-effort: literal path
// references only — variable expansion ($HOME/.meept/dev_key), command
// substitution and encoding bypass a string match, so the HARD guarantees
// are the file_read guard (guardSecretReadPath) and the engine path_rules
// (~/.meept/dev_key DecisionBlock); this pattern just makes the obvious
// `cat ~/.meept/dev_key` fail at risk classification.
var credentialPathPattern = regexp.MustCompile(
	`(?i)\.meept[/\\](dev_key|keys[/\\]|secrets[/\\])`,
)

// shellMetaPattern matches shell metacharacters that can chain commands.
// This includes: ; | && || $() ` ` > < >> << and newlines.
var shellMetaPattern = regexp.MustCompile(`[;&|<>]|&&|\|\||[` + "`" + `]|\$\(|\n`)

// FenceChecker validates paths against fence boundaries.
type FenceChecker interface {
	CheckPath(path string, op string) error
	CheckCommand(cmd string, workDir string) error
}

// ShellExecuteTool executes shell commands in a sandboxed subprocess.
type ShellExecuteTool struct {
	tools.ToolDefaults
	// workingDir is the tool's EXPLICIT working-directory fallback for callers that
	// are not bound to a session working directory (the terminal service's
	// configured dir). It is EMPTY when the caller passed nothing — the daemon
	// constructs the agent-path tool with "" and every per-session call then
	// resolves the directory from the tool context.
	//
	// Issue #136: an empty workingDir used to be replaced by
	// resolvePath(ctx, "~") at construction, which made "unset" and "explicitly
	// HOME" indistinguishable and ran `shell pwd` in the daemon operator's home
	// directory instead of the session worktree.
	workingDir        string
	defaultTimeout    time.Duration
	securityOrch      *intsecurity.Orchestrator
	knownSafeCommands map[string]struct{}
	containerMgr      *runtime.ContainerManager
	backend           runtime.ExecutionBackend
	logger            *slog.Logger
	ptyMgr            *pty.Manager
	fenceChecker      FenceChecker
	secretScanner     *intsecurity.SecretScanner

	// permissionTable is the declarative shell permission table. When set,
	// it SUPERSEDES knownSafeCommands: deny/ask short-circuit execution;
	// allow proceeds (tirith still runs). Both mechanisms remain available.
	permissionTable *intsecurity.PermissionTable

	// spillWriter is the tool-layer spill seam (nil = no spill; oversized
	// output is still tail-truncated, just without the re-readable pointer).
	// Wired by the daemon to llm.SpillService via SetSpillWriter.
	spillWriter SpillWriter
}

// SetSpillWriter installs the tool-layer spill writer (nil = no spill; the
// shell tool still tail-truncates oversized output, just without the
// re-readable pointer). Nil-guarded per the repo setter invariant.
func (t *ShellExecuteTool) SetSpillWriter(w SpillWriter) {
	if w != nil {
		t.spillWriter = w
	}
}

// shellExplicitWorkingDir reports whether the shell tool was constructed with
// an EXPLICIT working directory.
//
// Issue #136: NewShellExecuteTool used to replace an empty workingDir with
// resolvePath(ctx, "~") — the process HOME. That made "explicitly configured"
// indistinguishable from the daemon-wide home fallback, and it is exactly the
// daemon-wide fallback the AGENTS.md invariant forbids ("never fall back to the
// daemon's CWD"; $HOME is the same class of guess). The field is now left EMPTY
// when the caller passes nothing, so "no explicit dir" and "explicit dir" are
// representable again and the resolution helper below can tell them apart.
func (t *ShellExecuteTool) shellExplicitWorkingDir() bool {
	if t == nil {
		return false
	}
	// The pre-#136 constructor stored resolvePath(ctx, "~") for an empty
	// argument, so a long-lived tool built by an older wiring path (or a test
	// that pinned the old semantics) carries a HOME-shaped path in the field.
	// That value is a daemon-wide guess, not an explicit configuration: never
	// treat it as one. Matching is done on the FIELD SHAPE (a `~`-expansion),
	// not on equality with os.UserHomeDir(), because the session working
	// directory is itself often a home subdirectory (macOS test temp dirs live
	// under /var/folders, but real worktrees routinely live under $HOME).
	if t.workingDir == "" || isHomeExpansion(t.workingDir) {
		return false
	}
	return true
}

// isHomeExpansion reports whether path is (a symlink-resolved spelling of) the
// process home directory itself — the value NewShellExecuteTool used to store
// for an empty workingDir. It is a heuristic on the OLD sentinel value only:
// any other configured directory (including one under $HOME) stays explicit.
func isHomeExpansion(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil || path == "" || home == "" {
		return false
	}
	if path == home {
		return true
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	return realPath == realHome
}

// resolveShellWorkingDir resolves the directory a shell invocation must run in,
// in this precedence order:
//
//  1. args["working_dir"] when the caller supplied one (resolved via resolvePath
//     so ~ and relative-to-session forms keep working);
//  2. the session working directory injected by the agent loop
//     (tools.ContextWithWorkingDir) — the live, per-session binding;
//  3. t.workingDir ONLY when it was explicitly configured at construction;
//  4. otherwise an actionable tools.NoWorkingDirError("shell").
//
// Step 4 is the point of the fix: a shell call with nothing bound must REFUSE
// with the detectable sentinel, never silently run in the process home.
//
// rule is the caller's label for error messages ("shell" / "shell pty") and
// exists only so the sentinel text can name the failing surface.
func (t *ShellExecuteTool) resolveShellWorkingDir(ctx context.Context, args map[string]any, rule string) (string, error) {
	if wd, ok := args["working_dir"].(string); ok && strings.TrimSpace(wd) != "" {
		resolved, err := resolvePath(ctx, wd)
		if err != nil {
			return "", fmt.Errorf("invalid working directory: %w", err)
		}
		return resolved, nil
	}
	if wd := tools.WorkingDirFromContext(ctx); wd != "" {
		return wd, nil
	}
	if t.shellExplicitWorkingDir() {
		return t.workingDir, nil
	}
	return "", tools.NoWorkingDirError(rule)
}

// NewShellExecuteTool creates a new shell execution tool.
//
// workingDir is the tool's EXPLICIT fallback for callers that are not bound to
// a session working directory (the terminal service's configured dir). An empty
// value stays empty: the daemon constructs the agent-path tool with "" and
// every per-session call then resolves the directory from the tool context
// (issue #136 — the former "~" fallback ran `shell pwd` in the daemon
// operator's home directory instead of the session worktree).
func NewShellExecuteTool(workingDir string, defaultTimeout time.Duration, ptyMgr *pty.Manager) *ShellExecuteTool {
	if defaultTimeout == 0 {
		defaultTimeout = DefaultShellTimeout
	}
	return &ShellExecuteTool{
		workingDir:        workingDir,
		defaultTimeout:    defaultTimeout,
		knownSafeCommands: make(map[string]struct{}),
		ptyMgr:            ptyMgr,
		logger:            slog.Default().With("component", "shell-tool"),
	}
}

// SetSecurityOrchestrator sets the security orchestrator for command scanning.
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *ShellExecuteTool) SetSecurityOrchestrator(orch *intsecurity.Orchestrator) {
	if orch != nil {
		t.securityOrch = orch
	}
}

// SetPermissionTable installs the declarative shell permission table
// ([security.shell_permissions]). When configured it supersedes
// knownSafeCommands: a deny blocks execution, ask returns an error requiring
// user confirmation, and allow proceeds to tirith scanning as usual.
func (t *ShellExecuteTool) SetPermissionTable(pt *intsecurity.PermissionTable) {
	t.permissionTable = pt
}

// classifyForGuard adapts the builtin's locally-owned risk classifier to the
// shared guard's classifier seam. The classifier stays here (rather than in
// internal/security) because it depends on config-driven knownSafeCommands;
// the daemon wires this same classifier into scheduler-fired jobs through the
// shared guard option. The returned base command is used in the block message.
func (t *ShellExecuteTool) classifyForGuard(command string) (intsecurity.RiskLevel, string) {
	baseCmd := extractBaseCommand(command)
	switch t.classifyRisk(command) {
	case RiskCritical:
		return intsecurity.RiskCritical, baseCmd
	case RiskHigh:
		return intsecurity.RiskHigh, baseCmd
	default:
		return intsecurity.RiskMedium, baseCmd
	}
}

// guard builds a shared two-phase guard from the tool's current state. It is
// constructed per call so setters (SetPermissionTable, SetFenceChecker,
// SetSecurityOrchestrator, SetKnownSafeCommands) always take effect without a
// stale cache. The fence is injected as a function because builtin holds a
// FenceChecker interface (tests inject doubles), not a *FenceChecker.
//
// Network-class application is deliberately NOT configured here: the builtin
// tool still applies the class at exec time via confineDirect/backend, so
// doing it in the guard would double-apply. Scheduler-fired jobs, which spawn
// via exec.CommandContext directly, get the applier wired by the daemon.
func (t *ShellExecuteTool) guard() *intsecurity.ShellCommandGuardImpl {
	var fence intsecurity.FenceCheckFunc
	if t.fenceChecker != nil {
		fc := t.fenceChecker
		fence = fc.CheckCommand
	}
	return intsecurity.NewShellCommandGuard(
		nil,
		t.permissionTable,
		t.securityOrch,
		intsecurity.WithShellGuardFenceChecker(fence),
		intsecurity.WithShellGuardClassifier(t.classifyForGuard),
	)
}

// runtimeCommand is the launch record. NetworkClass is resolved here, before
// backend.Execute, from the tool context the executor stamps.
func (t *ShellExecuteTool) runtimeCommand(ctx context.Context, command, workDir string, timeout time.Duration) runtime.Command {
	return runtime.Command{
		Cmd:          command,
		Dir:          workDir,
		Timeout:      timeout,
		NetworkClass: tools.NetworkClassFromContext(ctx),
	}
}

// confineDirect applies the network class to a direct (no-backend) exec.
// A confinement error refuses the launch.
func confineDirect(cmd *exec.Cmd, class string) error {
	if err := runtime.ApplyNetworkClass(cmd, class); err != nil {
		return fmt.Errorf("%w: %w", security.ErrCommandBlocked, err)
	}
	return nil
}

// blockNetworkLaunch wraps a backend error that is a network-class refusal
// so the caller sees a guard block, not a transport failure.
func blockNetworkLaunch(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, runtime.ErrNetworkClassDenied) || errors.Is(err, runtime.ErrNetworkClassUnenforceable) {
		return fmt.Errorf("%w: %w", security.ErrCommandBlocked, err)
	}
	return nil
}

// SetFenceChecker sets the fence checker for path-based sandboxing.
func (t *ShellExecuteTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

// SetSecretScanner sets the secret scanner for output scanning.
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *ShellExecuteTool) SetSecretScanner(scanner *intsecurity.SecretScanner) {
	if scanner != nil {
		t.secretScanner = scanner
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

// SetBackend overrides backend-based execution with an arbitrary
// ExecutionBackend (used by daemon startup to install the sandbox-resolved
// or refusing backend). Passing nil is a no-op so accidental misuse cannot
// clear a previously-wired backend.
func (t *ShellExecuteTool) SetBackend(be runtime.ExecutionBackend) {
	if be == nil {
		return
	}
	t.backend = be
}

// SetRefusingBackend overrides backend-based execution with a refusing
// backend whose every Execute returns err. Used by daemon startup to enforce
// fail-closed semantics when sandbox resolution fails under
// require_sandbox=true: commands are REFUSED (error wrapping
// runtime.ErrSandboxRequired) rather than silently run unsandboxed via direct
// exec. Passing a nil error is a no-op so accidental misuse cannot disable
// execution.
func (t *ShellExecuteTool) SetRefusingBackend(err error) {
	if err == nil {
		return
	}
	t.backend = &refusingBackend{reason: err}
}

// Backend returns the currently-wired execution backend (nil when commands
// run via direct exec). Exported for diagnostics and startup wiring tests.
func (t *ShellExecuteTool) Backend() runtime.ExecutionBackend {
	return t.backend
}

// BackendWired reports whether a backend-based executor is currently wired
// (exported so daemon wiring can avoid double-wiring).
func (t *ShellExecuteTool) BackendWired() bool {
	return t.backend != nil
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
	// FullOutputPath is the absolute path of a spilled copy of the FULL
	// (pre-truncation) combined output. Set only when Truncated is true and
	// the spill write succeeded; empty when the output was under the cap or
	// the spill write failed (best-effort — the tool never fails on it).
	FullOutputPath string `json:"full_output_path,omitempty"`
}

// SpillWriter is the tool-layer spill seam. The shell tool spills oversized
// (truncated) output to a re-readable file; the daemon wires the real
// implementation (llm.SpillService) here. Empty session -> "nosession" dir.
// A failing writer must NOT fail the tool: callers treat WriteToolSpill as
// best-effort (log warn, omit the pointer, still return the tail).
type SpillWriter interface {
	// WriteToolSpill writes data to $MEEPT_HOME/spill/tools/<session_id|nosession>/<id>.txt
	// and returns the absolute path.
	WriteToolSpill(sessionID string, data []byte) (string, error)
}

// commandSemantic describes how to interpret non-zero exit codes for a
// specific command. Many CLI tools use exit code 1 for non-error conditions
// (e.g. grep returns 1 when no matches are found).
type commandSemantic struct {
	isError func(exitCode int) bool
	message func(exitCode int) string
}

// commandSemantics maps base command names to their exit-code semantics.
// Commands not listed here use the default rule: exit 0 = success, non-zero = error.
var commandSemantics = map[string]commandSemantic{
	"grep": {
		isError: func(c int) bool { return c >= 2 },
		message: func(c int) string {
			if c == 1 {
				return "no matches found"
			}
			return ""
		},
	},
	"rg": {
		isError: func(c int) bool { return c >= 2 },
		message: func(c int) string {
			if c == 1 {
				return "no matches found"
			}
			return ""
		},
	},
	"diff": {
		isError: func(c int) bool { return c >= 2 },
		message: func(c int) string {
			if c == 1 {
				return "files differ"
			}
			return ""
		},
	},
	"find": {
		isError: func(c int) bool { return c >= 2 },
		message: func(c int) string {
			if c == 1 {
				return "some directories were inaccessible"
			}
			return ""
		},
	},
	"test": {
		isError: func(c int) bool { return c >= 2 },
		message: func(c int) string {
			if c == 1 {
				return "condition is false"
			}
			return ""
		},
	},
}

// lastPipeCommand returns the base command of the last segment in a pipeline.
// For non-pipe commands it returns the base command directly.
func lastPipeCommand(command string) string {
	if segments, ok := splitOnUnquotedPipes(command); ok {
		last := strings.TrimSpace(segments[len(segments)-1])
		return extractBaseCommand(last)
	}
	return extractBaseCommand(command)
}

// interpretExitCode determines whether an exit code represents success or
// failure for the given command, applying command-specific semantics.
// Returns (success, semanticMessage). When success is true and
// semanticMessage is non-empty, the message explains the non-zero exit.
func interpretExitCode(command string, exitCode int) (success bool, message string) {
	if exitCode == 0 {
		return true, ""
	}
	baseCmd := lastPipeCommand(command)
	if sem, ok := commandSemantics[baseCmd]; ok {
		if !sem.isError(exitCode) {
			return true, sem.message(exitCode)
		}
	}
	return false, fmt.Sprintf("exit code %d", exitCode)
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

	// Command-only security guards run BEFORE working-directory resolution
	// (order contract of 86e4fc05, restored after the #136 reorder): a
	// refusal by the risk classifier or the permission table is an
	// authorization outcome and must surface as ErrCommandBlocked (-> 403
	// at the HTTP layer) even when the caller has no bound working
	// directory — otherwise the unbound-directory refusal (-> 400) shadows
	// the guard verdict and the always-denied list stops binding on
	// unbound lanes.
	//
	// Build the shared two-phase guard from the tool's current wiring.
	guard := t.guard()

	// Phase 1 (command-only checks: risk classification + permission table)
	// runs via the shared guard BEFORE working-directory resolution. A block
	// is ErrCommandBlocked (-> 403) even on an unbound lane, preserving the
	// frozen 403-before-400 ordering (see contract above).
	if err := guard.CheckCommand(command); err != nil {
		return nil, err
	}

	// Parse working directory (issue #136): explicit arg > session context binding
	// > explicitly configured tool dir > actionable refusal. Never the process
	// home, never the daemon's CWD.
	workDir, err := t.resolveShellWorkingDir(ctx, args, "shell")
	if err != nil {
		return nil, err
	}

	// Phase 2 (needs the resolved workDir: fence + Tirith scan) runs via the
	// shared guard after resolution but before spawn. A fence refusal is
	// still ErrCommandBlocked -> 403. Network-class application stays at
	// exec time below (the guard is not configured with an applier here).
	if err := guard.CheckCommandInDir(ctx, command, workDir); err != nil {
		return nil, err
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
		// Route through runtime backend. Network class is on the command
		// so Execute can confine before the child starts.
		result, err := t.backend.Execute(ctx, t.runtimeCommand(ctx, command, workDir, timeout))
		if err != nil {
			cancel()
			if blocked := blockNetworkLaunch(err); blocked != nil {
				return nil, blocked
			}
			return nil, fmt.Errorf("backend execution failed: %w", err)
		}
		// Backend returns combined output
		stdoutStr = result.Output
		returnCode = result.ExitCode
	} else {
		// Direct exec (original behavior)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir = workDir
		if err := confineDirect(cmd, tools.NetworkClassFromContext(ctx)); err != nil {
			cancel()
			return nil, err
		}

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

	// Pre-truncation combined output (stdout+stderr as captured). Used for the
	// evidence hash (full-output stability) and the best-effort spill so the
	// model can re-read bytes the tail window drops.
	combinedRaw := stdoutStr + stderrStr

	// Tail-keep truncation: oversized streams keep their TAIL (cut on a line
	// boundary, byte-safe) rather than the head — the head is what loses the
	// answer in build/test output. When either stream truncates, the FULL
	// combined output is spilled once (best-effort, never fails the tool);
	// the path rides in ShellResult.FullOutputPath and in the truncation
	// note.
	truncated = false
	fullOutputPath := ""
	if len(stdoutStr) > MaxOutputSize {
		kept, omitted := tailKeep([]byte(stdoutStr), MaxOutputSize)
		fullOutputPath = t.spillCombined(ctx, combinedRaw)
		stdoutStr = truncationNote(string(kept), len(stdoutStr), omitted, fullOutputPath)
		truncated = true
	}
	if len(stderrStr) > MaxOutputSize {
		kept, omitted := tailKeep([]byte(stderrStr), MaxOutputSize)
		if fullOutputPath == "" {
			// stdout did not truncate: spill the full combined output once.
			fullOutputPath = t.spillCombined(ctx, combinedRaw)
		}
		// Reuse the already-spilled path when stdout truncated first, so
		// one call never writes the same bytes twice.
		stderrStr = truncationNote(string(kept), len(stderrStr), omitted, fullOutputPath)
		truncated = true
	}

	// Sanitize output for prompt-injection patterns if the security
	// orchestrator is configured. An attacker who can influence command
	// output (environment variables, injected files, etc.) could otherwise
	// smuggle instructions into the agent context.
	stdoutStr = t.sanitizeOutput(command, stdoutStr)
	stderrStr = t.sanitizeOutput(command, stderrStr)

	// Scan output for potential secrets and prepend warning if found.
	if t.secretScanner != nil {
		if warning := t.secretScanner.ScanAndReport(stdoutStr + stderrStr); warning != "" {
			stdoutStr = warning + "\n" + stdoutStr
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

	// Hash the FULL pre-truncation combined output (not a head-50KB slice)
	// so the evidence hash is stable across truncation settings and mirrors
	// the transcript tool's full-text hash.
	h := sha256.Sum256([]byte(combinedRaw))
	outputHash := hex.EncodeToString(h[:])
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceShellOutput,
		command,
		outputHash,
		t.Name(),
	))

	success, semanticMsg := interpretExitCode(command, returnCode)
	if semanticMsg != "" {
		stdoutStr = stdoutStr + "\n[" + semanticMsg + "]"
	}

	return tools.ToolResult{
		Success: success,
		Result: ShellResult{
			Stdout:         stdoutStr,
			Stderr:         stderrStr,
			ReturnCode:     returnCode,
			Truncated:      truncated,
			FullOutputPath: fullOutputPath,
		},
		Evidence:   evidence,
		TaintLabel: taint.TaintExternal, // shell-sourced content is externally tainted (arbitrary user-commanded processes)
	}, nil
}

// tailKeep returns the last upTo bytes of data, cut on a newline boundary so
// a multi-byte UTF-8 rune is never split (byte-safe: the cut point is either
// 0, a newline index, or len(data) — all rune-safe positions). The second
// return value is the number of bytes omitted from the head. If data is <=
// upTo bytes, no truncation happens and the whole data is returned with
// omitted == 0.
//
// When the last upTo bytes contain no newline (a single giant line), the
// entire tail window is returned uncut — splitting a rune there would be
// worse than keeping the line whole.
func tailKeep(data []byte, upTo int) (kept []byte, omitted int) {
	if len(data) <= upTo {
		return data, 0
	}
	cut := len(data) - upTo
	// Backtrack to the newline just before the cut point, if one exists in
	// the window, so the kept tail starts on a fresh line.
	if nl := bytes.LastIndexByte(data[cut:], '\n'); nl >= 0 {
		cut += nl + 1
	}
	omitted = cut
	kept = data[cut:]
	return kept, omitted
}

// truncationNote builds the model-visible truncation line per C1:
//
//	... (truncated, N bytes total; first M bytes omitted[; full output: path])
//
// path is the spilled full-output location, appended only when non-empty
// (spill failed or unwired -> no pointer). The note is prepended to the
// kept tail so the model sees the omission up front.
func truncationNote(keptTail string, total, omitted int, fullOutputPath string) string {
	note := fmt.Sprintf("... (truncated, %d bytes total; first %d bytes omitted", total, omitted)
	if fullOutputPath != "" {
		note += "; full output: " + fullOutputPath
	}
	note += ")"
	return note + "\n" + keptTail
}

// sanitizeOutput runs the shell output through the InputSanitizer when a
// security orchestrator is wired. It returns the (possibly modified) text
// with prompt-injection patterns neutralised.
func (t *ShellExecuteTool) sanitizeOutput(command, output string) string {
	if t.securityOrch == nil {
		return output
	}
	sanitizer := t.securityOrch.InputSanitizer()
	if sanitizer == nil {
		return output
	}
	result := sanitizer.Sanitize(output)
	if result.WasModified || len(result.ThreatsDetected) > 0 {
		slog.Debug("shell output sanitized",
			"command", command,
			"threats", len(result.ThreatsDetected),
			"modified", result.WasModified)
	}
	return result.CleanText
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

	// Command-only security guards run BEFORE working-directory resolution
	// (order contract of 86e4fc05, restored after the #136 reorder; same
	// shape as Execute above): a refusal by the risk classifier or the
	// permission table is an authorization outcome and must surface as
	// ErrCommandBlocked (-> 403 at the HTTP layer) even when the caller
	// has no bound working directory.
	//
	// Build the shared two-phase guard from the tool's current wiring.
	guard := t.guard()

	// Phase 1 (command-only checks: risk classification + permission table)
	// runs via the shared guard BEFORE working-directory resolution (same
	// shape as Execute above), preserving the 403-before-400 ordering.
	if err := guard.CheckCommand(command); err != nil {
		return nil, err
	}

	// Parse working directory (issue #136): same resolution as Execute — explicit
	// arg > session context binding > explicitly configured tool dir >
	// actionable refusal.
	workDir, err := t.resolveShellWorkingDir(ctx, args, "shell")
	if err != nil {
		return nil, err
	}

	// Phase 2 (needs the resolved workDir: fence + Tirith scan) runs via the
	// shared guard after resolution but before spawn. A fence refusal is
	// still ErrCommandBlocked -> 403.
	if err := guard.CheckCommandInDir(ctx, command, workDir); err != nil {
		return nil, err
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

		result, err := t.backend.Execute(ctx, t.runtimeCommand(ctx, command, workDir, timeout))
		if err != nil {
			if blocked := blockNetworkLaunch(err); blocked != nil {
				return nil, blocked
			}
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
		if err := confineDirect(cmd, tools.NetworkClassFromContext(ctx)); err != nil {
			return nil, err
		}

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

	// Tail-keep truncation (streaming lane — same GAP-1 class as Execute).
	// The full combined output is spilled once, best-effort, when either
	// stream truncates; the path rides in ShellResult.FullOutputPath and in
	// the truncation note.
	combinedRaw := stdoutStr + stderrStr
	fullOutputPath := ""
	if len(stdoutStr) > MaxOutputSize {
		kept, omitted := tailKeep([]byte(stdoutStr), MaxOutputSize)
		fullOutputPath = t.spillCombined(ctx, combinedRaw)
		stdoutStr = truncationNote(string(kept), len(stdoutStr), omitted, fullOutputPath)
		truncated = true
	}
	if len(stderrStr) > MaxOutputSize {
		kept, omitted := tailKeep([]byte(stderrStr), MaxOutputSize)
		if fullOutputPath == "" {
			fullOutputPath = t.spillCombined(ctx, combinedRaw)
		}
		stderrStr = truncationNote(string(kept), len(stderrStr), omitted, fullOutputPath)
		truncated = true
	}

	// Sanitize output for prompt-injection patterns (same as Execute).
	stdoutStr = t.sanitizeOutput(command, stdoutStr)
	stderrStr = t.sanitizeOutput(command, stderrStr)

	// Scan output for potential secrets and prepend warning if found.
	if t.secretScanner != nil {
		if warning := t.secretScanner.ScanAndReport(stdoutStr + stderrStr); warning != "" {
			stdoutStr = warning + "\n" + stdoutStr
		}
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
	// Hash the FULL pre-truncation combined output (stable across truncation
	// settings), mirroring the Execute lane.
	h := sha256.Sum256([]byte(combinedRaw))
	outputHash := hex.EncodeToString(h[:])
	evidence = append(evidence, models.NewEvidence(
		models.EvidenceShellOutput,
		command,
		outputHash,
		t.Name(),
	))

	success, semanticMsg := interpretExitCode(command, returnCode)
	if semanticMsg != "" {
		stdoutStr = stdoutStr + "\n[" + semanticMsg + "]"
	}

	return tools.ToolResult{
		Success: success,
		Result: ShellResult{
			Stdout:         stdoutStr,
			Stderr:         stderrStr,
			ReturnCode:     returnCode,
			Truncated:      truncated,
			FullOutputPath: fullOutputPath,
		},
		Evidence:   evidence,
		TaintLabel: taint.TaintExternal, // shell-sourced content is externally tainted (arbitrary user-commanded processes)
	}, nil
}

// spillCombined writes the full pre-truncation combined output to the
// tool-spill dir (best-effort). Returns the absolute path on success, or
// "" when no spiller is wired or the write fails. The shell tool NEVER fails
// on a spill failure: truncation still applies, only the re-readable pointer
// is omitted (logged at warn).
func (t *ShellExecuteTool) spillCombined(ctx context.Context, combined string) string {
	if t.spillWriter == nil || combined == "" {
		return ""
	}
	sid := shellSpillSessionID(ctx)
	path, err := t.spillWriter.WriteToolSpill(sid, []byte(combined))
	if err != nil {
		if logger := t.logger; logger != nil {
			logger.Warn("tool output spill write failed; tail-truncated result has no re-readable pointer",
				"error", err)
		}
		return ""
	}
	return path
}

// shellSpillSessionID extracts the agent session id for tool-spill
// subdirectory scoping. It returns "" (NOT the browser "default") when the
// context carries no session, so the spill sanitizer maps it to the
// "nosession" directory (leaf contract: empty session -> "nosession").
func shellSpillSessionID(ctx context.Context) string {
	if sid := tools.SessionIDFromContext(ctx); sid != "" {
		return sid
	}
	if sid, ok := ctx.Value(sessionIDContextKey).(string); ok && sid != "" {
		return sid
	}
	return ""
}

// classifyRisk determines the risk level of a command.
// classifyRiskSimple is a helper that classifies a simple command without
// re-splitting on shell metacharacters (used to avoid infinite recursion).
func (t *ShellExecuteTool) classifyRiskSimple(command string) ShellCommandRisk {
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

	// Daemon credential material (audit M5 Phase 2c) — same check as
	// classifyRisk, applied per pipe/semicolon segment so a chained
	// `echo x | cat ~/.meept/dev_key` cannot hide the reference.
	if credentialPathPattern.MatchString(command) {
		return RiskCritical
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

	// Daemon credential material (audit M5 Phase 2c): any command naming
	// dev_key/keys/secrets is CRITICAL — blocked outright at the call
	// sites. Checked on the FULL command (not per segment) and before the
	// readOnly downgrade so `cat ~/.meept/dev_key` can never ride the
	// read-only allowlist at MEDIUM.
	if credentialPathPattern.MatchString(command) {
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
			segRisk := t.classifyRiskSimple(strings.TrimSpace(seg))
			if segRisk > maxRisk {
				maxRisk = segRisk
			}
		}
		return maxRisk
	}

	// SECURITY FIX (2026-07-14): Detect shell metacharacters that can chain
	// commands. If present, split on semicolons and &&/|| to evaluate each
	// command segment independently. This prevents bypasses like:
	// `cat /etc/passwd; rm -rf /`
	// We use classifyRiskSimple to avoid infinite recursion.
	if shellMetaPattern.MatchString(command) {
		// Split on semicolons first
		semiSegments := strings.Split(command, ";")
		maxRisk := RiskMedium
		for _, seg := range semiSegments {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			// Further split on && and ||
			andOrSegments := splitOnLogicalOperators(seg)
			for _, subSeg := range andOrSegments {
				subSeg = strings.TrimSpace(subSeg)
				if subSeg == "" {
					continue
				}
				subRisk := t.classifyRiskSimple(subSeg)
				if subRisk > maxRisk {
					maxRisk = subRisk
				}
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

// splitOnLogicalOperators splits a command on && and || operators.
func splitOnLogicalOperators(cmd string) []string {
	var segments []string
	current := cmd
	for {
		idxAnd := strings.Index(current, "&&")
		idxOr := strings.Index(current, "||")
		if idxAnd == -1 && idxOr == -1 {
			if current != "" {
				segments = append(segments, current)
			}
			break
		}
		// Find which comes first.
		// splitIdx > 0 here: -1 is impossible because at least one of the two
		// indexes above is >= 0, and an operator at position 0 is impossible
		// because an earlier iteration consumed the preceding segment.
		splitIdx := idxAnd
		if idxAnd == -1 || (idxOr != -1 && idxOr < idxAnd) {
			splitIdx = idxOr
		}
		if splitIdx > 0 {
			segments = append(segments, current[:splitIdx])
		}
		current = current[splitIdx+2:]
	}
	return segments
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
//
// The interface carries no context, so this lane resolves the directory from
// config.Dir first and then the tool's EXPLICITLY configured dir — never the
// process home. Issue #136: the former code fell back to t.workingDir, which
// used to hold resolvePath(ctx, "~"), so an unbound PTY ran in $HOME while the
// fence was asked about $HOME too (fencing the wrong root). When nothing is
// configured the session REFUSES with the actionable sentinel rather than
// inheriting the daemon's (or the operator's home) directory.
func (t *ShellExecuteTool) CreateSession(sessionID string, config tools.PTYSessionConfig) (*tools.PTYSessionInfo, error) {
	// SECURITY FIX (2026-07-14): Always check fence boundaries before creating
	// PTY sessions. Previously, the fence check was skipped when config.Dir
	// was empty, allowing commands to execute outside the workspace.
	// When Dir is empty, the command runs in the tool's configured working dir.
	checkDir := config.Dir
	if checkDir == "" && t.shellExplicitWorkingDir() {
		checkDir = t.workingDir
	}
	if checkDir == "" {
		return nil, tools.NoWorkingDirError("shell pty")
	}
	if t.fenceChecker != nil {
		if err := t.fenceChecker.CheckCommand(config.Cmd, checkDir); err != nil {
			return nil, fmt.Errorf("pty session rejected by fence: %w", err)
		}
	}

	if t.ptyMgr == nil {
		return nil, fmt.Errorf("PTY manager not available")
	}

	ptyCfg := pty.SessionConfig{
		Cmd:  config.Cmd,
		Args: config.Args,
		Dir:  checkDir,
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
		Dir:       ptyCfg.Dir,
		CreatedAt: time.Now(),
		Rows:      ptyCfg.Rows,
		Cols:      ptyCfg.Cols,
		IsRunning: sess.IsRunning(),
	}, nil
}

// WriteToSession sends input to a PTY session.
// SEC-H4 FIX (2026-07-14): Validate input for dangerous escape sequences
// that could be used to break out of the intended command context.
func (t *ShellExecuteTool) WriteToSession(sessionID string, input []byte) error {
	if t.ptyMgr == nil {
		return fmt.Errorf("PTY manager not available")
	}

	sess := t.ptyMgr.GetSession(sessionID)
	if sess == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Log dangerous escape sequences that could enable shell escape
	// This is defense-in-depth; fence checking at session creation is primary boundary
	dangerousPatterns := [][]byte{
		[]byte("\x03"), // Ctrl-C (interrupt)
		[]byte("\x1a"), // Ctrl-Z (suspend)
		[]byte("\x1c"), // Ctrl-\ (quit)
		[]byte("\x1b"), // Escape key
	}
	for _, pattern := range dangerousPatterns {
		if bytes.Contains(input, pattern) {
			t.logger.Debug("PTY input contains control sequence",
				"session", sessionID,
				"pattern", fmt.Sprintf("%x", pattern))
		}
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

// IsReadOnly reports whether the shell command is a read-only operation.
// It uses the existing risk classification and read-only command table to
// make an input-dependent determination.
func (t *ShellExecuteTool) IsReadOnly(input map[string]any) bool {
	cmd, _ := input["command"].(string)
	// Redirects (>, >>) make any command a write operation.
	if strings.Contains(cmd, ">") {
		return false
	}
	return t.classifyRisk(cmd) <= RiskMedium && readOnlyCommands[extractBaseCommand(cmd)]
}

// IsConcurrencySafe reports whether the shell command can safely execute
// concurrently. Only read-only commands are considered concurrency-safe.
func (t *ShellExecuteTool) IsConcurrencySafe(input map[string]any) bool {
	return t.IsReadOnly(input)
}
