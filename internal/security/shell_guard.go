// Package security: shared two-phase shell command guard.
//
// This is the single execution-time guard used by both the builtin shell tool
// (internal/tools/builtin) and scheduler-fired shell jobs (internal/scheduler).
// Putting it here — rather than in either consumer — is the only cycle-safe
// home: builtin imports scheduler, and pkg/security cannot import internal/.
//
// The two phases mirror the frozen ordering of the builtin shell tool:
//
//  1. CheckCommand(command) — command-only checks (risk classification and
//     the declarative permission table). Runs BEFORE work-directory
//     resolution so a block surfaces as ErrCommandBlocked (HTTP 403) rather
//     than being shadowed by an unbound-directory refusal (HTTP 400).
//  2. CheckCommandInDir(ctx, command, workDir) — checks that need the resolved
//     working directory (fence, Tirith scan) plus optional network-class
//     application. Runs AFTER resolution but BEFORE the child is spawned.
//
// Every dependency is optional and nil-safe: a nil fence/table/orchestrator
// means that check is skipped, matching the builtin tool's legacy behavior
// when those components are not wired.
package security

import (
	"context"
	"fmt"

	pkgsecurity "github.com/caimlas/meept/pkg/security"
)

// ShellGuardClassifier classifies a shell command for the shared guard. It
// returns the risk level and the base command name (the latter is used in the
// block message). Keeping the classifier behind an option lets consumers with
// config-driven allowlists (notably the builtin tool's knownSafeCommands)
// supply their own logic without this package importing them.
type ShellGuardClassifier func(command string) (risk RiskLevel, baseCommand string)

// FenceCheckFunc is the fence-check seam for phase 2. It is a function rather
// than a *FenceChecker so consumers that hold a fence interface (and inject
// test doubles) can delegate without this package taking on that interface.
// When the real *FenceChecker is passed to NewShellCommandGuard, the default
// calls FenceChecker.CheckCommand — it never reimplements fence logic.
type FenceCheckFunc func(command, workDir string) error

// ShellCommandGuard is the two-phase execution-time fence shared by the
// builtin shell tool and scheduler-fired shell jobs.
//
// Phase 1 (CheckCommand) is risk/permission classification and runs before
// work-dir resolution; phase 2 (CheckCommandInDir) is the fence/Tirith/
// network-class check and runs after resolution. Both must run BEFORE the
// command is spawned. Consumers may declare a matching local interface — Go's
// structural typing connects them without an import.
type ShellCommandGuard interface {
	CheckCommand(command string) error
	CheckCommandInDir(ctx context.Context, command, workDir string) error
}

// ShellGuardOption configures a ShellCommandGuardImpl. Options are applied in
// order at construction; nil options are ignored.
type ShellGuardOption func(*ShellCommandGuardImpl)

// ShellCommandGuardImpl is the concrete two-phase guard. Construct with
// NewShellCommandGuard; the zero value is also safe (all checks skipped).
type ShellCommandGuardImpl struct {
	fence      FenceCheckFunc
	table      *PermissionTable
	orch       *Orchestrator
	classifier ShellGuardClassifier
}

var _ ShellCommandGuard = (*ShellCommandGuardImpl)(nil)

// NewShellCommandGuard builds a two-phase guard from the optional security
// components. Any of fc/pt/orch may be nil — the corresponding check is then
// skipped. Functional options inject the classifier and network-class seams
// that consumers need to preserve their own behavior.
func NewShellCommandGuard(fc *FenceChecker, pt *PermissionTable, orch *Orchestrator, opts ...ShellGuardOption) *ShellCommandGuardImpl {
	g := &ShellCommandGuardImpl{
		table: pt,
		orch:  orch,
	}
	if fc != nil {
		fcLocal := fc
		g.fence = func(command, workDir string) error {
			return fcLocal.CheckCommand(command, workDir)
		}
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

// WithShellGuardClassifier installs the risk classifier used by phase 1. A
// nil classifier is ignored (risk classification is then skipped).
func WithShellGuardClassifier(fn ShellGuardClassifier) ShellGuardOption {
	return func(g *ShellCommandGuardImpl) {
		if fn != nil {
			g.classifier = fn
		}
	}
}

// WithShellGuardFenceChecker installs the phase-2 fence seam. It overrides the
// *FenceChecker passed to NewShellCommandGuard. A nil checker is ignored.
func WithShellGuardFenceChecker(fn FenceCheckFunc) ShellGuardOption {
	return func(g *ShellCommandGuardImpl) {
		if fn != nil {
			g.fence = fn
		}
	}
}

// CheckCommand runs the command-only checks: risk classification
// (RiskCritical blocks) and the declarative permission table (deny/ask block).
// It MUST run before work-directory resolution to preserve the 403-before-400
// ordering. All blocks wrap pkg/security.ErrCommandBlocked.
func (g *ShellCommandGuardImpl) CheckCommand(command string) error {
	if g == nil {
		return nil
	}
	if g.classifier != nil {
		risk, baseCmd := g.classifier(command)
		if risk >= RiskCritical {
			if baseCmd == "" {
				baseCmd = command
			}
			return fmt.Errorf("%w: command blocked for safety: %s", pkgsecurity.ErrCommandBlocked, baseCmd)
		}
	}
	return CheckShellPermission(g.table, command)
}

// CheckShellPermission consults a declarative shell permission table and
// returns a guard block for deny/ask. Shared by the guard's phase 1 and by the
// builtin tool's compatibility wrapper so the two cannot drift.
func CheckShellPermission(pt *PermissionTable, command string) error {
	if pt == nil {
		return nil
	}
	decision, prefix, ok := pt.Evaluate(command)
	if !ok {
		return nil
	}
	switch decision {
	case ShellActionDeny:
		return fmt.Errorf("%w: command denied by shell permission table rule: %s", pkgsecurity.ErrCommandBlocked, prefix)
	case ShellActionAsk:
		return fmt.Errorf("%w: command requires user confirmation by shell permission table rule: %s", pkgsecurity.ErrCommandBlocked, prefix)
	default: // allow: downstream checks still run (defense in depth)
		return nil
	}
}

// CheckCommandInDir runs the checks that need the resolved working directory:
// the fence (delegating to FenceChecker.CheckCommand), the Tirith orchestrator
// scan, and optional network-class application. It MUST run after resolution
// but BEFORE the child is spawned. All blocks wrap
// pkg/security.ErrCommandBlocked.
func (g *ShellCommandGuardImpl) CheckCommandInDir(ctx context.Context, command, workDir string) error {
	if g == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if g.fence != nil {
		if err := g.fence(command, workDir); err != nil {
			return fmt.Errorf("%w: fence: %w", pkgsecurity.ErrCommandBlocked, err)
		}
	}

	if g.orch != nil {
		blocked, _, reason := g.orch.ScanShellCommand(ctx, command)
		if blocked {
			return fmt.Errorf("%w: command blocked by security scanner: %s", pkgsecurity.ErrCommandBlocked, reason)
		}
	}

	return nil
}
