// Package security provides security-related functionality for meept.
package security

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const tirithTimeout = 2 * time.Second

// tirithAvailable caches whether tirith is available.
var (
	tirithOnce      sync.Once
	tirithAvailable bool
)

// TirithResult represents the result of a tirith scan.
type TirithResult struct {
	Blocked  bool    // True only for BLOCKED findings
	Warning  bool    // True for WARNING findings (logged, not blocked)
	Severity *string // e.g. "CRITICAL", "MEDIUM"
	RuleID   *string // e.g. "non_ascii_hostname"
	Message  *string // Full tirith output
}

// detailRE extracts [SEVERITY] rule_id from tirith output.
var detailRE = regexp.MustCompile(`\[(\w+)]\s+(\S+)`)

// CheckTirithAvailable checks whether the tirith binary is reachable on PATH.
func CheckTirithAvailable(ctx context.Context, binary string) bool {
	if binary == "" {
		binary = "tirith"
	}

	tirithOnce.Do(func() {
		ctx, cancel := context.WithTimeout(ctx, tirithTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, binary, "--version")
		if err := cmd.Run(); err == nil {
			tirithAvailable = true
		}
	})

	return tirithAvailable
}

// ScanCommand runs tirith check on a shell command and parses the output.
// Returns nil if tirith is not installed (graceful degradation).
func ScanCommand(ctx context.Context, command string, binary string) *TirithResult {
	if binary == "" {
		binary = "tirith"
	}

	if !CheckTirithAvailable(ctx, binary) {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, tirithTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "check", "--", command)
	output, err := cmd.CombinedOutput()

	// If timeout or other error, allow execution (graceful degradation)
	if ctx.Err() != nil || err != nil {
		// Check if it's just a non-zero exit code (expected for blocked/warning)
		if _, ok := err.(*exec.ExitError); !ok && err != nil {
			return nil
		}
	}

	outputStr := string(output)

	var severity, ruleID *string

	// Extract severity and rule_id
	for _, line := range strings.Split(outputStr, "\n") {
		if m := detailRE.FindStringSubmatch(line); len(m) == 3 {
			s, r := m[1], m[2]
			severity = &s
			ruleID = &r
			break
		}
	}

	var message *string
	if trimmed := strings.TrimSpace(outputStr); trimmed != "" {
		message = &trimmed
	}

	if strings.Contains(outputStr, "BLOCKED") {
		return &TirithResult{
			Blocked:  true,
			Warning:  false,
			Severity: severity,
			RuleID:   ruleID,
			Message:  message,
		}
	}

	if strings.Contains(outputStr, "WARNING") {
		return &TirithResult{
			Blocked:  false,
			Warning:  true,
			Severity: severity,
			RuleID:   ruleID,
			Message:  message,
		}
	}

	// Clean scan
	return &TirithResult{
		Blocked:  false,
		Warning:  false,
		Severity: nil,
		RuleID:   nil,
		Message:  message,
	}
}

// TirithScanner provides an interface for scanning commands.
type TirithScanner struct {
	binary string
}

// NewTirithScanner creates a new TirithScanner.
func NewTirithScanner(binary string) *TirithScanner {
	if binary == "" {
		binary = "tirith"
	}
	return &TirithScanner{binary: binary}
}

// IsAvailable checks if tirith is available.
func (t *TirithScanner) IsAvailable(ctx context.Context) bool {
	return CheckTirithAvailable(ctx, t.binary)
}

// Scan scans a command for security issues.
func (t *TirithScanner) Scan(ctx context.Context, command string) *TirithResult {
	return ScanCommand(ctx, command, t.binary)
}

// ShouldBlock returns true if the command should be blocked.
func (t *TirithScanner) ShouldBlock(ctx context.Context, command string) bool {
	result := t.Scan(ctx, command)
	return result != nil && result.Blocked
}
