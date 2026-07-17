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
// TurnRecord / LLMMessage / TurnSnapshot -- atomic snapshot types for HALO-style recovery
// -----------------------------------------------------------------------

// TurnRecord represents a single tool invocation in a turn snapshot.
type TurnRecord struct {
	Name       string         `json:"name"`
	Input      map[string]any `json:"input,omitempty"`
	Output     string         `json:"output,omitempty"`
	Success    bool           `json:"success"`
	DurationMs int            `json:"duration_ms,omitempty"`
}

// LLMMessage represents one message in the LLM exchange for a turn.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TurnSnapshot is an atomic record of one agent turn: tool calls,
// LLM messages, and outputs. Written atomically for crash recovery
// and post-hoc analysis.
//
// Modeled after HALO's turn compaction pattern (engine/main.py).
type TurnSnapshot struct {
	Sequence    int            `json:"sequence"`
	TurnID      string         `json:"turn_id"`
	ToolCalls   []TurnRecord   `json:"tool_calls"`
	LLMMessages []LLMMessage   `json:"llm_messages"`
	Output      string         `json:"output"`
	Timestamp   time.Time      `json:"timestamp"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TurnSnapshotFile is the on-disk JSON wrapper for a TurnSnapshot plus version header.
type TurnSnapshotFile struct {
	Version  int           `json:"version"`
	Snapshot TurnSnapshot   `json:"snapshot"`
}

const TurnSnapshotVersion = 1

// -----------------------------------------------------------------------
// AtomicTurnLogger -- per-directory snapshot persistence
// -----------------------------------------------------------------------

// AtomicTurnLogger writes and reads TurnSnapshots with atomic file
// operations (write to temp, then rename) for crash safety.
type AtomicTurnLogger struct {
	basePath string
	current  int
	mu       sync.Mutex
}

// NewAtomicTurnLogger creates a logger that stores snapshots under basePath.
// Creates the directory if it does not exist.
func NewAtomicTurnLogger(basePath string) (*AtomicTurnLogger, error) {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, fmt.Errorf("create turn snapshot dir: %w", err)
	}
	return &AtomicTurnLogger{
		basePath: basePath,
		current:  0,
	}, nil
}

// NextSequenceAtom increments the sequence counter and returns the next
// value.
func (l *AtomicTurnLogger) NextSequence() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.current++
	return l.current
}

// WriteSnapshot atomically writes a TurnSnapshot to disk.
//
// The file is written to a temporary path in the same directory and
// renamed into place so that any crash mid-write leaves the previous
// (or non-existent) snapshot untouched.
//
// File naming convention: <turn_id>.json (uses sequence when turn_id is
// empty).
func (l *AtomicTurnLogger) WriteSnapshot(snap TurnSnapshot) error {
	snap.Timestamp = time.Now()
	if snap.TurnID == "" {
		snap.TurnID = id.Generate("turn_")
	}
	if snap.Metadata == nil {
		snap.Metadata = make(map[string]string)
	}
	snap.Metadata["version"] = fmt.Sprintf("%d", TurnSnapshotVersion)

	out := TurnSnapshotFile{
		Version:  TurnSnapshotVersion,
		Snapshot: snap,
	}

	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	filename := snap.TurnID
	if filename == "" {
		filename = fmt.Sprintf("turn_%d.json", snap.Sequence)
	}
	filename = sanitizeFilename(filename) + ".json"

	targetPath := filepath.Join(l.basePath, filename)
	return atomicWriteFile(targetPath, data, 0o644)
}

// ReadSnapshot reads a TurnSnapshot by turn ID.
func (l *AtomicTurnLogger) ReadSnapshot(turnID string) (*TurnSnapshot, error) {
	filename := sanitizeFilename(turnID) + ".json"
	path := filepath.Join(l.basePath, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot %s: %w", turnID, err)
	}

	var file TurnSnapshotFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot %s: %w", turnID, err)
	}

	return &file.Snapshot, nil
}

// ListSnapshots returns all available turn snapshots sorted by sequence number.
func (l *AtomicTurnLogger) ListSnapshots() ([]TurnSnapshot, error) {
	entries, err := os.ReadDir(l.basePath)
	if err != nil {
		return nil, fmt.Errorf("read snapshot dir: %w", err)
	}

	var snapshots []TurnSnapshot
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		snap, err := readSnapshotFromFile(filepath.Join(l.basePath, e.Name()))
		if err != nil {
			continue // skip corrupted files
		}
		snapshots = append(snapshots, snap)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Sequence < snapshots[j].Sequence
	})

	return snapshots, nil
}

// PruneSnapshots keeps only the last N snapshots by sequence, deleting
// older ones from disk. Returns the number of snapshots deleted.
func (l *AtomicTurnLogger) PruneSnapshots(keepLast int) (int, error) {
	snapshots, err := l.ListSnapshots()
	if err != nil {
		return 0, err
	}
	if len(snapshots) <= keepLast {
		return 0, nil
	}

	toDelete := snapshots[:len(snapshots)-keepLast]
	deleted := 0
	for _, s := range toDelete {
		p := filepath.Join(l.basePath, sanitizeFilename(s.TurnID)+".json")
		if err := os.Remove(p); err != nil {
			continue
		}
		deleted++
	}
	return deleted, nil
}

// LastSequence returns the highest sequence number seen across all
// persisted snapshots. Returns 0 if no snapshots exist.
func (l *AtomicTurnLogger) LastSequence() int {
	snapshots, err := l.ListSnapshots()
	if err != nil || len(snapshots) == 0 {
		return 0
	}
	return snapshots[len(snapshots)-1].Sequence
}

// -----------------------------------------------------------------------
// TurnCompactor -- context-window optimization (preserved from Phase 6)
// -----------------------------------------------------------------------

// Turn represents a single agent turn during RLM analysis.
type Turn struct {
	Type       string
	ToolName   string
	ToolInput  map[string]any
	Content    string
	TokenCount int
}

// CompactTurn represents a compacted turn record.
// Merges tool_call + tool_response into single record for ~40% context reduction.
// Modeled after HALO's turn compaction pattern (engine/main.py).
type CompactTurn struct {
	Type       string         `json:"type"` // "tool", "thinking", "final"
	ToolName   string         `json:"tool_name,omitempty"`
	ToolInput  map[string]any `json:"tool_input,omitempty"`
	ToolOutput string         `json:"tool_output,omitempty"`
	Thinking   string         `json:"thinking,omitempty"` // collapsed thinking
	Content    string         `json:"content,omitempty"`
	TokenCount int            `json:"token_count"`
}

// TurnCompactor compacts agent turns for efficient context window usage.
type TurnCompactor struct {
	maxThinkingTurns int // max consecutive thinking turns before collapse
}

// NewTurnCompactor creates a compactor with sensible defaults.
func NewTurnCompactor() *TurnCompactor {
	return &TurnCompactor{
		maxThinkingTurns: 2,
	}
}

// CompactTurns merges consecutive tool_call + tool_response pairs
// and collapses redundant thinking turns.
// Returns ~40% reduction in context size for typical RLM runs.
func (tc *TurnCompactor) CompactTurns(turns []Turn) []CompactTurn {
	if len(turns) == 0 {
		return nil
	}

	var compacted []CompactTurn
	var pendingTool *CompactTurn
	var thinkingBuffer []string

	for _, turn := range turns {
		switch turn.Type {
		case "tool_call":
			pendingTool = &CompactTurn{
				Type:       "tool",
				ToolName:   turn.ToolName,
				ToolInput:  turn.ToolInput,
				TokenCount: turn.TokenCount,
			}

		case "tool_response":
			if pendingTool != nil {
				pendingTool.ToolOutput = turn.Content
				pendingTool.TokenCount += turn.TokenCount
				compacted = append(compacted, *pendingTool)
				pendingTool = nil
			}

		case "thinking":
			thinkingBuffer = append(thinkingBuffer, turn.Content)

		case "final":
			if len(thinkingBuffer) > 0 {
				var thinkingText string
				if len(thinkingBuffer) > tc.maxThinkingTurns {
					thinkingText = collapseThinking(thinkingBuffer)
				} else {
					thinkingText = strings.Join(thinkingBuffer, "\n")
				}
				compacted = append(compacted, CompactTurn{
					Type:       "thinking",
					Thinking:   thinkingText,
					TokenCount: sumTokenCounts(turns),
				})
			}
			compacted = append(compacted, CompactTurn{
				Type:       "final",
				Content:    turn.Content,
				TokenCount: turn.TokenCount,
			})
		}
	}

	return compacted
}

// -----------------------------------------------------------------------
// internals
// -----------------------------------------------------------------------

// readSnapshotFromFile reads and returns a TurnSnapshot from a JSON file.
func readSnapshotFromFile(path string) (TurnSnapshot, error) {
	var file TurnSnapshotFile
	data, err := os.ReadFile(path)
	if err != nil {
		return TurnSnapshot{}, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return TurnSnapshot{}, err
	}
	return file.Snapshot, nil
}

// atomicWriteFile writes data to targetPath atomically via a temp file
// + rename. If the rename fails the temp file is cleaned up.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	name := filepath.Base(path)

	f, err := os.CreateTemp(dir, name+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	return nil
}

func sanitizeFilename(name string) string {
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.' {
			return r
		}
		return '_'
	}, name)
	for strings.Contains(sanitized, "__") {
		sanitized = strings.ReplaceAll(sanitized, "__", "_")
	}
	return sanitized
}

func collapseThinking(buffer []string) string {
	if len(buffer) <= 2 {
		return strings.Join(buffer, "\n")
	}
	middle := summarizeMiddle(buffer[1 : len(buffer)-1])
	return buffer[0] + "\n[...]\n" + middle + "\n[...]\n" + buffer[len(buffer)-1]
}

func summarizeMiddle(middle []string) string {
	return fmt.Sprintf("[thinking: %d turns omitted]", len(middle))
}

func sumTokenCounts(turns []Turn) int {
	total := 0
	for _, t := range turns {
		total += t.TokenCount
	}
	return total
}
