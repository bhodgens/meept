package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// -----------------------------------------------------------------------
// Checkpoint -- full agent state snapshot for crash recovery
// -----------------------------------------------------------------------

// Checkpoint captures the complete state of an agent run so it can be
// resumed after a crash, timeout, or manual cancellation.
//
// Modeled after HALO's turn-compaction recovery: checkpoints are written
// periodically (controlled by checkpointInterval) and contain enough
// state to resume from the last completed turn.
type Checkpoint struct {
	// RunID is the unique identifier for the agent run.
	RunID string `json:"run_id"`
	// AgentID identifies the employee / agent that owns this run.
	AgentID string `json:"agent_id"`
	// Depth is the recursion/subagent depth at the time of the checkpoint.
	Depth int `json:"depth,omitempty"`
	// TurnCounter tracks the current turn and the limit.
	TurnCounter struct {
		Current int `json:"current"`
		Limit   int `json:"limit"`
	} `json:"turn_counter"`
	// ToolCalls captures all tool invocations in the current turn.
	ToolCalls []TurnRecord `json:"tool_calls"`
	// LLMMessages captures the LLM conversation history up to this point.
	LLMMessages []LLMMessage `json:"llm_messages"`
	// Outputs holds the final output text from the last completed turn.
	Outputs string `json:"outputs,omitempty"`
	// Timestamp records when the checkpoint was written.
	Timestamp time.Time `json:"timestamp"`
	// Metadata is an optional free-form map for extra context.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// checkpointFile is the on-disk JSON wrapper for a Checkpoint plus version header.
type checkpointFile struct {
	Version     int        `json:"version"`
	Checkpoint  Checkpoint `json:"checkpoint"`
	// Final is set to true when the run completed successfully (sentinel).
	Final bool `json:"final,omitempty"`
}

const CheckpointVersion = 1

const checkpointFinalSentinel = "__final__"
const checkpointPrefix = "checkpoint_"

// -----------------------------------------------------------------------
// ResumeResult -- outcome of a recovery resume
// -----------------------------------------------------------------------

// ResumeResult describes what happened when an incomplete run was resumed
// from a checkpoint.
type ResumeResult struct {
	// Restored is true when a checkpoint was successfully loaded.
	Restored bool `json:"restored"`
	// Checkpoint is the loaded checkpoint that was resumed from.
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"`
	// SkippedTurns is the number of turns that were already completed
	// before the crash/interruption and will be skipped during resume.
	SkippedTurns int `json:"skipped_turns"`
	// RunID is the ID of the run that was resumed.
	RunID string `json:"run_id"`
	// State is the restored agent state (caller-defined interface{}).
	State interface{} `json:"-"`
	// ResumedAt records when the resume operation occurred.
	ResumedAt time.Time `json:"resumed_at"`
	// Warning contains any non-fatal issues discovered during resume.
	Warning string `json:"warning,omitempty"`
}

// -----------------------------------------------------------------------
// RunRecoverer -- periodic checkpoint writer and crash-recovery manager
// -----------------------------------------------------------------------

// RunRecoverer manages periodic checkpoints for agent runs and provides
// crash-recovery capabilities. It stores data under a base directory,
// creating one checkpoint file per incomplete run plus a sentinel for
// completed runs.
type RunRecoverer struct {
	basePath         string
	checkpointInterval int // checkpoint every N turns (0 = disabled)
	mu               sync.Mutex
}

// NewRunRecoverer creates a recoverer that stores checkpoints under basePath.
// checkpointInterval controls how often checkpoints are written (every N turns).
// Set to 0 to disable automatic checkpointing (manual writes only).
func NewRunRecoverer(basePath string, checkpointInterval int) (*RunRecoverer, error) {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, fmt.Errorf("create recoverer directory: %w", err)
	}
	return &RunRecoverer{
		basePath:           basePath,
		checkpointInterval: checkpointInterval,
	}, nil
}

// -----------------------------------------------------------------------
// WriteCheckpoint -- atomic checkpoint persistence
// -----------------------------------------------------------------------

// WriteCheckpoint writes a checkpoint atomically to disk.
//
// The file is written to a temporary path and renamed into place so that
// any crash mid-write leaves either the previous checkpoint or no
// checkpoint, but never a partially-written one.
//
// Checkpoint files are named <checkpointPrefix><runID>.json.
func (r *RunRecoverer) WriteCheckpoint(cp *Checkpoint) error {
	if cp == nil {
		return fmt.Errorf("checkpoint is nil")
	}
	if cp.RunID == "" {
		cp.RunID = id.Generate("run_")
	}
	if cp.Timestamp.IsZero() {
		cp.Timestamp = time.Now()
	}
	if cp.Metadata == nil {
		cp.Metadata = make(map[string]string)
	}
	cp.Metadata["version"] = fmt.Sprintf("%d", CheckpointVersion)
	cp.Metadata["recoverer"] = "meept-agent"

	out := checkpointFile{
		Version:    CheckpointVersion,
		Checkpoint: *cp,
	}

	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	filename := fmt.Sprintf("%s%s.json", checkpointPrefix, cp.RunID)
	targetPath := filepath.Join(r.basePath, filename)
	return atomicWriteFile(targetPath, data, 0o644)
}

// MarkComplete writes an empty sentinel file that marks a run as finished,
// distinguishing it from incomplete (crashed) runs.
// The previous checkpoint file is renamed to include ".done" suffix.
func (r *RunRecoverer) MarkComplete(runID string) error {
	// Rename the existing checkpoint to .done
	oldName := fmt.Sprintf("%s%s.json", checkpointPrefix, runID)
	oldPath := filepath.Join(r.basePath, oldName)

	doneName := fmt.Sprintf("%s%s.done", checkpointPrefix, runID)
	donePath := filepath.Join(r.basePath, doneName)

	if err := os.Rename(oldPath, donePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("move checkpoint to .done: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------------
// ReadCheckpoint -- load a checkpoint by run ID
// -----------------------------------------------------------------------

// ReadCheckpoint loads a checkpoint by run ID.
func (r *RunRecoverer) ReadCheckpoint(runID string) (*Checkpoint, error) {
	filename := fmt.Sprintf("%s%s.json", checkpointPrefix, runID)
	path := filepath.Join(r.basePath, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read checkpoint %s: %w", runID, err)
	}

	var file checkpointFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint %s: %w", runID, err)
	}

	return &file.Checkpoint, nil
}

// -----------------------------------------------------------------------
// ListIncompleteRuns -- discover interrupted runs
// -----------------------------------------------------------------------

// ListIncompleteRuns returns run IDs for runs that have an active
// checkpoint (not yet marked complete via MarkComplete).
func (r *RunRecoverer) ListIncompleteRuns() ([]string, error) {
	entries, err := os.ReadDir(r.basePath)
	if err != nil {
		return nil, fmt.Errorf("read recoverer directory: %w", err)
	}

	var incomplete []string
	for _, e := range entries {
		name := e.Name()
		// Active checkpoint files: checkpoint_<runID>.json
		if strings.HasPrefix(name, checkpointPrefix) && strings.HasSuffix(name, ".json") {
			runID := strings.TrimSuffix(strings.TrimPrefix(name, checkpointPrefix), ".json")
			if runID != "" {
				incomplete = append(incomplete, runID)
			}
		}
	}

	sort.Strings(incomplete)
	return incomplete, nil
}

// ListCompletedRuns returns run IDs for runs that have been marked complete
// via MarkComplete (they have .done sentinel files).
func (r *RunRecoverer) ListCompletedRuns() ([]string, error) {
	entries, err := os.ReadDir(r.basePath)
	if err != nil {
		return nil, fmt.Errorf("read recoverer directory: %w", err)
	}

	var completed []string
	for _, e := range entries {
		name := e.Name()
		// Completed sentinel files: checkpoint_<runID>.done
		if strings.HasPrefix(name, checkpointPrefix) && strings.HasSuffix(name, ".done") {
			runID := strings.TrimSuffix(strings.TrimPrefix(name, checkpointPrefix), ".done")
			if runID != "" {
				completed = append(completed, runID)
			}
		}
	}

	sort.Strings(completed)
	return completed, nil
}

// -----------------------------------------------------------------------
// Resume -- restore state from last checkpoint
// -----------------------------------------------------------------------

// Resume loads the latest checkpoint for an incomplete run and returns
// a ResumeResult describing what was restored.
func (r *RunRecoverer) Resume(incompleteRunID string) (*ResumeResult, error) {
	cp, err := r.ReadCheckpoint(incompleteRunID)
	if err != nil {
		return nil, fmt.Errorf("resume checkpoint for run %s: %w", incompleteRunID, err)
	}

	return &ResumeResult{
		Restored:     true,
		Checkpoint:   cp,
		SkippedTurns: cp.TurnCounter.Current,
		RunID:        cp.RunID,
		State:        nil, // caller populates this
		ResumedAt:    time.Now(),
		Warning:      r.computeWarning(cp),
	}, nil
}

// ResumeFromLatest resumes from the most recent checkpoint across all
// incomplete runs. Returns the result and the run ID that was resumed from.
func (r *RunRecoverer) ResumeFromLatest() (*ResumeResult, string, error) {
	incomplete, err := r.ListIncompleteRuns()
	if err != nil {
		return nil, "", fmt.Errorf("list incomplete runs: %w", err)
	}
	if len(incomplete) == 0 {
		return nil, "", fmt.Errorf("no incomplete runs found")
	}

	// Pick the most recent by reading timestamps from checkpoint metadata.
	bestID := incomplete[0]
	var bestTime time.Time
	for _, runID := range incomplete {
		cp, err := r.ReadCheckpoint(runID)
		if err != nil {
			continue
		}
		if cp.Timestamp.After(bestTime) {
			bestTime = cp.Timestamp
			bestID = runID
		}
	}

	result, err := r.Resume(bestID)
	return result, bestID, err
}

// computeWarning checks for common issues that might affect resume.
func (r *RunRecoverer) computeWarning(cp *Checkpoint) string {
	var issues []string

	if cp.TurnCounter.Current == 0 {
		issues = append(issues, "checkpoint taken at turn 0 — no turns to skip")
	}

	if cp.TurnCounter.Limit > 0 && cp.TurnCounter.Current >= cp.TurnCounter.Limit {
		issues = append(issues, "checkpoint was taken after turn limit was reached — resume may complete immediately")
	}

	// Check for stale data (older than 7 days).
	if !cp.Timestamp.IsZero() && time.Since(cp.Timestamp) > 7*24*time.Hour {
		issues = append(issues, "checkpoint is older than 7 days and may be stale")
	}

	if len(issues) == 0 {
		return ""
	}
	return strings.Join(issues, "; ")
}

// -----------------------------------------------------------------------
// ShouldCheckpoint -- periodic checkpoint decision
// -----------------------------------------------------------------------

// ShouldCheckpoint returns true when a checkpoint should be written for
// the given turn number based on the recoverer's checkpointInterval.
//
// When checkpointInterval is 0 (disabled), this always returns false.
// Otherwise it returns true when turn > 0 and turn % interval == 0.
func (r *RunRecoverer) ShouldCheckpoint(turn int) bool {
	if r.checkpointInterval <= 0 {
		return false
	}
	return turn > 0 && turn%r.checkpointInterval == 0
}

// -----------------------------------------------------------------------
// Cleanup -- remove old checkpoint artifacts
// -----------------------------------------------------------------------

// CleanupCompleted removes completed run artifacts (both .json and .done
// files) for the given run IDs. Returns the number of files removed.
func (r *RunRecoverer) CleanupCompleted(runIDs ...string) (int, error) {
	removed := 0
	for _, runID := range runIDs {
		baseName := fmt.Sprintf("%s%s", checkpointPrefix, runID)
		for _, suffix := range []string{".json", ".done"} {
			path := filepath.Join(r.basePath, baseName+suffix)
			if err := os.Remove(path); err != nil {
				if os.IsNotExist(err) {
					continue // file didn't exist, don't count
				}
				continue // other error, don't count
			}
			removed++
		}
	}
	return removed, nil
}

// CleanupAllCompleted removes ALL completed run artifacts.
// Returns error and the number of files removed.
func (r *RunRecoverer) CleanupAllCompleted() (int, error) {
	entries, err := os.ReadDir(r.basePath)
	if err != nil {
		return 0, fmt.Errorf("read recoverer directory: %w", err)
	}

	removed := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, checkpointPrefix) && strings.HasSuffix(name, ".done") {
			runID := strings.TrimSuffix(strings.TrimPrefix(name, checkpointPrefix), ".done")
			ids := []string{runID}
			n, _ := r.CleanupCompleted(ids...)
			removed += n
		}
	}
	return removed, nil
}

// CleanupBefore removes all incomplete checkpoints older than the given cutoff.
// Returns the number of files removed.
func (r *RunRecoverer) CleanupBefore(cutoff time.Time) (int, error) {
	incomplete, err := r.ListIncompleteRuns()
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, runID := range incomplete {
		cp, err := r.ReadCheckpoint(runID)
		if err != nil {
			continue
		}
		if !cp.Timestamp.IsZero() && cp.Timestamp.Before(cutoff) {
			if _, err := r.CleanupCompleted(runID); err != nil {
				continue
			}
			removed++
		}
	}
	return removed, nil
}

// -----------------------------------------------------------------------
// GetCheckpointInterval -- access checkpoint interval
// -----------------------------------------------------------------------

// GetCheckpointInterval returns the configured checkpoint interval.
func (r *RunRecoverer) GetCheckpointInterval() int {
	return r.checkpointInterval
}

// SetCheckpointInterval updates the checkpoint interval.
func (r *RunRecoverer) SetCheckpointInterval(interval int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkpointInterval = interval
}

// -----------------------------------------------------------------------
// Helpers for integration
// -----------------------------------------------------------------------

// SnapshotForTurn converts a set of turn records and LLM messages into a
// Checkpoint suitable for WriteCheckpoint.
func SnapshotForTurn(runID, agentID string, depth int, currentTurn, limit int,
	toolCalls []TurnRecord, messages []LLMMessage, output string,
) *Checkpoint {
	// Deep copy tool calls
	tc := make([]TurnRecord, len(toolCalls))
	copy(tc, toolCalls)

	// Deep copy messages
	msgs := make([]LLMMessage, len(messages))
	copy(msgs, messages)

	return &Checkpoint{
		RunID:   runID,
		AgentID: agentID,
		Depth:   depth,
		TurnCounter: struct {
			Current int `json:"current"`
			Limit   int `json:"limit"`
		}{Current: currentTurn, Limit: limit},
		ToolCalls:     tc,
		LLMMessages:   msgs,
		Outputs:       output,
		Timestamp:     time.Now(),
		Metadata:      nil,
	}
}

// IncompleteRunCount returns the number of incomplete (not yet completed) runs.
func (r *RunRecoverer) IncompleteRunCount() (int, error) {
	runs, err := r.ListIncompleteRuns()
	if err != nil {
		return 0, err
	}
	return len(runs), nil
}

// IsComplete checks if a run has been marked complete.
func (r *RunRecoverer) IsComplete(runID string) bool {
	donePath := filepath.Join(r.basePath, fmt.Sprintf("%s%s.done", checkpointPrefix, runID))
	_, err := os.Stat(donePath)
	return !os.IsNotExist(err)
}

// -----------------------------------------------------------------------
// checkpointFile sentinel helper (for external use, matches HALO pattern)
// -----------------------------------------------------------------------

// FinalSentinelContent returns the filename component used as the final-run sentinel.
func FinalSentinelContent() string {
	return checkpointFinalSentinel
}
