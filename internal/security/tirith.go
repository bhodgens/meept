// Package security provides security-related functionality for meept.
package security

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const tirithTimeout = 2 * time.Second

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

// tirithAvailabilityCache caches availability by binary path.
// SEC-2 FIX: Per-binary caching instead of package-level Once.
var (
	tirithCacheMu  sync.RWMutex
	tirithCacheMap = make(map[string]bool)
)

// CheckTirithAvailable checks whether the tirith binary is reachable on PATH.
// SEC-2 FIX: Now caches per binary path instead of using a single package-level sync.Once.
func CheckTirithAvailable(ctx context.Context, binary string) bool {
	if binary == "" {
		binary = BinaryTirith
	}

	// Check cache first with read lock
	tirithCacheMu.RLock()
	available, cached := tirithCacheMap[binary]
	tirithCacheMu.RUnlock()

	if cached {
		return available
	}

	// Check availability and cache with write lock
	tirithCacheMu.Lock()
	defer tirithCacheMu.Unlock()

	// Double-check after acquiring write lock
	if available, cached = tirithCacheMap[binary]; cached {
		return available
	}

	ctx, cancel := context.WithTimeout(ctx, tirithTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "--version")
	available = cmd.Run() == nil
	tirithCacheMap[binary] = available

	return available
}

// ScanCommand runs tirith check on a shell command and parses the output.
// Returns nil if tirith is not installed (graceful degradation).
func ScanCommand(ctx context.Context, command, binary string) *TirithResult {
	if binary == "" {
		binary = BinaryTirith
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
		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) {
			return nil
		}
	}

	outputStr := string(output)

	var severity, ruleID *string

	// Extract severity and rule_id
	for line := range strings.SplitSeq(outputStr, "\n") {
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
		binary = BinaryTirith
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
