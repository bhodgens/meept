package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// CaptureRecorder intercepts agent tool calls and records them as
// ResearchTrajectory entries to raw_captures.jsonl.
type CaptureRecorder struct {
	dataDir      string
	includeTools map[string]struct{} // nil/empty = capture all tools
	mu           sync.Mutex
}

// NewCaptureRecorder creates a CaptureRecorder that writes captures into
// dataDir. The directory (and parents) are created if they don't exist.
// All tools are captured until Configure is called with an include list.
func NewCaptureRecorder(dataDir string) (*CaptureRecorder, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("learning: dataDir must not be empty")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("learning: mkdir %s: %w", dataDir, err)
	}
	return &CaptureRecorder{dataDir: dataDir}, nil
}

// Configure sets the tool allowlist used by RecordResearch. An empty or nil
// list means all tools are captured. Thread-safe.
func (c *CaptureRecorder) Configure(includeTools []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(includeTools) == 0 {
		c.includeTools = nil
		return
	}
	m := make(map[string]struct{}, len(includeTools))
	for _, t := range includeTools {
		if t != "" {
			m[t] = struct{}{}
		}
	}
	c.includeTools = m
}

// shouldCaptureTool reports whether toolName is allowed. Caller must hold c.mu
// only if reading includeTools without races; Configure holds the lock when
// writing, and we re-check under lock in RecordResearch.
func (c *CaptureRecorder) toolAllowed(toolName string) bool {
	if len(c.includeTools) == 0 {
		return true
	}
	_, ok := c.includeTools[toolName]
	return ok
}

// RecordResearch appends a per-tool-call capture entry to raw_captures.jsonl.
// The domain is classified from the query and tool output. This captures
// individual tool invocations in real time (non-blocking, cheap). Safe for
// concurrent use. Tools not in the configured allowlist are silently skipped.
//
// Note: per-tool captures intentionally omit Synthesis. Consolidation skips
// empty-synthesis entries so only full RecordTrajectory turns become training
// data; per-tool lines remain in the immutable raw log for auditing.
func (c *CaptureRecorder) RecordResearch(ctx context.Context, sessionID, query, toolName, toolOutput string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.toolAllowed(toolName) {
		return nil
	}

	domain := ClassifyDomain(query, toolOutput)
	trajectory := ResearchTrajectory{
		ID:        id.Generate("lcap-"),
		SessionID: sessionID,
		Domain:    domain,
		Query:     query,
		ToolCalls: []ToolCallRecord{
			{
				Tool:    toolName,
				Query:   query,
				Results: 1,
				Used:    true,
			},
		},
		Timestamp: time.Now().UTC(),
	}

	return c.appendTrajectory(trajectory)
}

// RecordTrajectory appends a complete research trajectory — the full
// (intent, query, synthesis, tool path, outcome) tuple — to
// raw_captures.jsonl. This is the rich capture format suitable for
// training data: it associates the user's original intent with the
// agent's final synthesis and the sequence of tools used. Call this
// at turn end (after the response is generated). Safe for concurrent use.
func (c *CaptureRecorder) RecordTrajectory(ctx context.Context, sessionID, intent, synthesis string, toolNames []string, success bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Classify from intent + synthesis (the richest signal for domain routing).
	domain := ClassifyDomain(intent, synthesis)

	toolCalls := make([]ToolCallRecord, 0, len(toolNames))
	for _, tn := range toolNames {
		// Trajectory captures the tools that ran this turn even if some would
		// not be allowlisted for per-tool RecordResearch — the turn-level
		// synthesis is what training needs.
		toolCalls = append(toolCalls, ToolCallRecord{
			Tool:    tn,
			Results: 1,
			Used:    true,
		})
	}

	trajectory := ResearchTrajectory{
		ID:        id.Generate("ltraj-"),
		SessionID: sessionID,
		Domain:    domain,
		Intent:    intent,
		Query:     intent,
		ToolCalls: toolCalls,
		Synthesis: synthesis,
		TaskOutcome: TaskOutcome{
			Success: success,
		},
		Timestamp: time.Now().UTC(),
	}

	return c.appendTrajectory(trajectory)
}

// appendTrajectory writes a ResearchTrajectory as one JSONL line.
// Caller must hold c.mu.
func (c *CaptureRecorder) appendTrajectory(trajectory ResearchTrajectory) error {
	capturesFile := filepath.Join(c.dataDir, "raw_captures.jsonl")
	f, err := os.OpenFile(capturesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("learning: open captures file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(trajectory)
	if err != nil {
		return fmt.Errorf("learning: marshal trajectory: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("learning: write capture: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("learning: write newline: %w", err)
	}
	return nil
}
