package agent

import "sync"

var fileModifyingTools = map[string]bool{
	"file_write":    true,
	"file_edit":     true,
	"file_delete":   true,
	"git_commit":    true,
	"shell_execute": true,
}

// VerificationTracker counts file-modifying tool calls and tracks which files
// were changed so the verification auto-trigger hook can nudge the agent to
// self-verify before reporting completion.
type VerificationTracker struct {
	mu           sync.Mutex
	editCount    int
	filesChanged []string
	threshold    int
}

// NewVerificationTracker creates a tracker that fires after threshold
// file-modifying tool calls. Thresholds below 1 default to 3.
func NewVerificationTracker(threshold int) *VerificationTracker {
	if threshold < 1 {
		threshold = 3
	}
	return &VerificationTracker{threshold: threshold}
}

// RecordToolCall increments the edit counter when toolName is a
// file-modifying tool. filePath is recorded when non-empty.
func (t *VerificationTracker) RecordToolCall(toolName string, filePath string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if fileModifyingTools[toolName] {
		t.editCount++
		if filePath != "" {
			t.filesChanged = append(t.filesChanged, filePath)
		}
	}
}

// ShouldTrigger reports whether the edit count has reached the threshold.
func (t *VerificationTracker) ShouldTrigger() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.editCount >= t.threshold
}

// Snapshot returns the list of changed files and resets the counter and file
// list so the next trigger cycle starts fresh.
func (t *VerificationTracker) Snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	files := make([]string, len(t.filesChanged))
	copy(files, t.filesChanged)
	t.editCount = 0
	t.filesChanged = nil
	return files
}

// Reset clears all tracked state without returning the file list.
func (t *VerificationTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.editCount = 0
	t.filesChanged = nil
}
