package agent

import (
	"fmt"
	"sync"
)

// PairChannel message types for bus-channel-based agent pairing.

// PairVerdict represents the outcome of a reviewer's evaluation.
type PairVerdict string

const (
	PairVerdictApproved  PairVerdict = "approved"
	PairVerdictRejected  PairVerdict = "rejected"
	PairVerdictNeedsMore PairVerdict = "needs_more"
)

// PairTurn represents a single turn in the pair conversation.
type PairTurn struct {
	// SessionID identifies the pair session.
	SessionID string `json:"session_id"`
	// TurnNumber is the sequential turn index (0-based).
	TurnNumber int `json:"turn_number"`
	// AgentID is the agent that produced this turn.
	AgentID string `json:"agent_id"`
	// Role is either "actor" or "reviewer".
	Role string `json:"role"`
	// Content is the agent's output text.
	Content string `json:"content"`
	// Verdict is the reviewer's classification (empty for actor turns).
	Verdict PairVerdict `json:"verdict,omitempty"`
	// Feedback is the reviewer's feedback (empty for approved or actor turns).
	Feedback string `json:"feedback,omitempty"`
}

// PairStartRequest initiates a new pair conversation session.
type PairStartRequest struct {
	// SessionID is the unique pair session identifier.
	SessionID string `json:"session_id"`
	// ActorID is the agent that performs the initial work.
	ActorID string `json:"actor_id"`
	// ReviewerID is the agent that reviews the actor's output.
	ReviewerID string `json:"reviewer_id"`
	// InitialPrompt is the user's original request.
	InitialPrompt string `json:"initial_prompt"`
	// MaxTurns is the maximum number of actor-reviewer cycles (0 = unlimited, default 5).
	MaxTurns int `json:"max_turns,omitempty"`
	// Metadata holds optional extra data from the dispatcher.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// BusPairSessionState represents the current state of a bus-channel pair session.
type BusPairSessionState struct {
	SessionID     string      `json:"session_id"`
	ActorID       string      `json:"actor_id"`
	ReviewerID    string      `json:"reviewer_id"`
	CurrentTurn   int         `json:"current_turn"`
	MaxTurns      int         `json:"max_turns"`
	Phase         string      `json:"phase"` // "actor_turn", "reviewer_turn", "completed", "failed"
	LastVerdict   PairVerdict `json:"last_verdict,omitempty"`
	Turns         []PairTurn  `json:"turns,omitempty"`
	InitialPrompt string      `json:"initial_prompt"`
	mu            sync.RWMutex
}

// BusPairSessionStateSnapshot is a mutex-free copy of BusPairSessionState
// for safe concurrent access. Returns from GetSession to avoid copying locks.
type BusPairSessionStateSnapshot struct {
	SessionID     string      `json:"session_id"`
	ActorID       string      `json:"actor_id"`
	ReviewerID    string      `json:"reviewer_id"`
	CurrentTurn   int         `json:"current_turn"`
	MaxTurns      int         `json:"max_turns"`
	Phase         string      `json:"phase"`
	LastVerdict   PairVerdict `json:"last_verdict,omitempty"`
	Turns         []PairTurn  `json:"turns,omitempty"`
	InitialPrompt string      `json:"initial_prompt"`
}

// PairResult is the final result of a completed pair session.
type PairResult struct {
	SessionID    string      `json:"session_id"`
	FinalOutput  string      `json:"final_output"`
	Turns        []PairTurn  `json:"turns"`
	TotalTurns   int         `json:"total_turns"`
	FinalVerdict PairVerdict `json:"final_verdict"`
}

// Bus topic constants for pair channel messages.
//
// DELIVERY CONTRACT: the bus (internal/bus) has no latched or buffered
// topics — Publish is fire-and-forget into the channels of subscribers
// registered AT PUBLISH TIME, and a message published with no subscriber
// (or a full subscriber buffer) is dropped. There is NO late-subscriber
// replay. This applies to all four pair topics below: pair.start,
// pair.{session}.turn, pair.result, and especially pair.error — a client
// that subscribes after the pair session has started will never see a
// pair.error emitted before it attached (this was the root cause of the
// TestPairOrchestrator_FullConversation flake; the test now subscribes
// before publishing). Real operators must subscribe to pair.* topics
// before initiating pair sessions. If guaranteed delivery is ever
// needed, add a latching topic kind to the bus itself — do NOT work
// around it at publish sites.
const (
	// TopicPairStart is used to initiate a pair session.
	TopicPairStart = "pair.start"
	// TopicPairTurnPattern is the per-session topic pattern: "pair.{sessionID}.turn"
	TopicPairTurnPattern = "pair.%s.turn"
	// TopicPairResult is published when a pair session completes.
	TopicPairResult = "pair.result"
	// TopicPairError is published when a pair session fails.
	TopicPairError = "pair.error"
)

// PairTopic returns the turn topic for a specific pair session.
func PairTopic(sessionID string) string {
	return fmt.Sprintf(TopicPairTurnPattern, sessionID)
}
