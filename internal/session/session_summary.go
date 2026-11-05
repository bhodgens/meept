package session

// The first built-in projection fold unit: `session-summary` (contract S2,
// leaf 03-session-storage/02 task T1). It feeds the TUI/GUI session list:
// message_count, last_role/last_text (a cheap preview), and has_response —
// the same signals the store already exposes via GetMessageCount /
// HasResponses, folded incrementally instead of re-queried per render.
//
// The View output is plain JSON-safe data (contract S2 whole-value rule).

import "fmt"

// SessionSummaryState is the folded summary for one session. It is the
// complete post-change value (never a delta).
type SessionSummaryState struct {
	MessageCount int    `json:"message_count"`
	HasResponse  bool   `json:"has_response"`
	LastRole     string `json:"last_role,omitempty"`
	LastText     string `json:"last_text,omitempty"`
}

// SessionSummaryKey is the registry key of the built-in summary unit.
const SessionSummaryKey = "session-summary"

// sessionSummaryStateVersion is bumped whenever SessionSummaryState's shape
// changes; persisted rows under an older version are discarded at read
// (never migrated) per contract S2.
const sessionSummaryStateVersion = 1

// summaryPreviewCap bounds the folded last_text preview (the full content
// stays in the store; the summary is a list-row signal).
const summaryPreviewCap = 240

// SessionSummaryDefinition returns the built-in `session-summary` fold unit
// for Register.
func SessionSummaryDefinition() ProjectionDefinition[string] {
	return ProjectionDefinition[string]{
		Key:          SessionSummaryKey,
		StateVersion: sessionSummaryStateVersion,
		Init: func(SessionVersionedMeta) SessionProjectionState {
			return &SessionSummaryState{}
		},
		Apply: func(state SessionProjectionState, msg Message) SessionProjectionState {
			s, ok := state.(*SessionSummaryState)
			if !ok {
				// Not our state (foreign unit reuse): same reference out.
				return state
			}
			if msg.EntryType != "" && msg.EntryType != KeyMessage {
				return s // fold real messages only
			}
			s.MessageCount++
			if msg.Role == "assistant" {
				s.HasResponse = true
			}
			s.LastRole = msg.Role
			text := msg.Content
			if len(text) > summaryPreviewCap {
				text = text[:summaryPreviewCap]
			}
			s.LastText = text
			return s
		},
		View: func(state SessionProjectionState) any {
			s, ok := state.(*SessionSummaryState)
			if !ok {
				return nil
			}
			out := *s // detached copy — never the live cell ref
			return &out
		},
	}
}

// RegisterSessionSummary is the convenience wiring for daemon/TUI startup:
// register the built-in summary unit on a fresh registry.
func RegisterSessionSummary(reg *SessionProjectionRegistry) (SessionProjectionHandle, error) {
	if reg == nil {
		return SessionProjectionHandle{}, fmt.Errorf("session: nil projection registry")
	}
	return reg.Register(SessionSummaryDefinition())
}
