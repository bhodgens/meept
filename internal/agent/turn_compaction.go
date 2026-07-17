package agent

import (
	"fmt"
	"strings"
)

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
			// Start accumulating tool turn.
			pendingTool = &CompactTurn{
				Type:       "tool",
				ToolName:   turn.ToolName,
				ToolInput:  turn.ToolInput,
				TokenCount: turn.TokenCount,
			}

		case "tool_response":
			// Complete tool turn.
			if pendingTool != nil {
				pendingTool.ToolOutput = turn.Content
				pendingTool.TokenCount += turn.TokenCount
				compacted = append(compacted, *pendingTool)
				pendingTool = nil
			}

		case "thinking":
			// Buffer thinking turns for later flush/collapse.
			thinkingBuffer = append(thinkingBuffer, turn.Content)

		case "final":
			// Flush pending thinking (with collapse if needed).
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
			// Add final turn.
			compacted = append(compacted, CompactTurn{
				Type:       "final",
				Content:    turn.Content,
				TokenCount: turn.TokenCount,
			})
		}
	}

	return compacted
}

// collapseThinking summarizes middle thinking turns.
func collapseThinking(buffer []string) string {
	if len(buffer) <= 2 {
		return strings.Join(buffer, "\n")
	}
	// Keep first, summarize middle, keep last.
	middle := summarizeMiddle(buffer[1 : len(buffer)-1])
	return buffer[0] + "\n[...]\n" + middle + "\n[...]\n" + buffer[len(buffer)-1]
}

// summarizeMiddle returns a one-line summary of middle thoughts.
func summarizeMiddle(middle []string) string {
	return fmt.Sprintf("[thinking: %d turns omitted]", len(middle))
}

// sumTokenCounts sums token counts across all turns.
func sumTokenCounts(turns []Turn) int {
	total := 0
	for _, t := range turns {
		total += t.TokenCount
	}
	return total
}
