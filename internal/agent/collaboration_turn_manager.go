package agent

import (
	"fmt"
	"sync"
	"time"
)

// TurnAction represents the action taken at the end of a turn.
type TurnAction string

const (
	TurnApprove        TurnAction = "approve"
	TurnRequestChanges TurnAction = "request_changes"
	TurnRequestToken   TurnAction = "request_token"
	TurnYield          TurnAction = "yield"
)

// TurnManager tracks editor token ownership and enforces turn limits.
type TurnManager struct {
	participants []string
	tokenHolder  int        // index into participants holding the editor token
	turnCount    int
	maxTurns     int
	maxTokens    int64  // per turn token limit
	turnTimeout  time.Duration

	mu sync.RWMutex
}

// NewTurnManager creates a new turn manager.
// participants: ordered list of agent IDs. Token starts with first participant.
func NewTurnManager(participants []string, maxTurns int, maxTokens int64, turnTimeout time.Duration) *TurnManager {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	if turnTimeout <= 0 {
		turnTimeout = 5 * time.Minute
	}
	return &TurnManager{
		participants: participants,
		tokenHolder:  0,
		maxTurns:     maxTurns,
		maxTokens:    maxTokens,
		turnTimeout:  turnTimeout,
	}
}

// TokenHolder returns the agent ID currently holding the editor token.
func (tm *TurnManager) TokenHolder() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if len(tm.participants) == 0 {
		return ""
	}
	return tm.participants[tm.tokenHolder]
}

// IsTokenHolder returns true if the given agent ID holds the token.
func (tm *TurnManager) IsTokenHolder(agentID string) bool {
	return tm.TokenHolder() == agentID
}

// Yield allows the current token holder to voluntarily pass the token.
// The token moves to the next participant in round-robin order.
func (tm *TurnManager) Yield() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if len(tm.participants) == 0 {
		return fmt.Errorf("no participants configured")
	}
	turn := tm.turnCount + 1
	if turn >= tm.maxTurns {
		return fmt.Errorf("max turns (%d) would be exceeded", tm.maxTurns)
	}
	tm.tokenHolder = (tm.tokenHolder + 1) % len(tm.participants)
	tm.turnCount = turn
	return nil
}

// RequestToken allows an observer to request the editor token.
// Returns true if the token was transferred, false otherwise.
func (tm *TurnManager) RequestToken(requesterID string) (bool, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.participants) == 0 {
		return false, fmt.Errorf("no participants configured")
	}

	// Find the requester's index
	reqIdx := -1
	for i, p := range tm.participants {
		if p == requesterID {
			reqIdx = i
			break
		}
	}
	if reqIdx == -1 {
		return false, fmt.Errorf("requester %s is not a participant", requesterID)
	}

	turn := tm.turnCount + 1
	if turn >= tm.maxTurns {
		return false, fmt.Errorf("max turns (%d) would be exceeded", tm.maxTurns)
	}

	tm.tokenHolder = reqIdx
	tm.turnCount = turn
	return true, nil
}

// ForceYield forcibly passes the token to the next participant.
// Used when max tokens or timeout is exceeded.
func (tm *TurnManager) ForceYield() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if len(tm.participants) == 0 {
		return fmt.Errorf("no participants configured")
	}
	turn := tm.turnCount + 1
	if turn >= tm.maxTurns {
		return fmt.Errorf("max turns (%d) reached; cannot force yield", tm.maxTurns)
	}
	tm.tokenHolder = (tm.tokenHolder + 1) % len(tm.participants)
	tm.turnCount = turn
	return nil
}

// CurrentTurn returns the current turn number (1-indexed).
func (tm *TurnManager) CurrentTurn() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.turnCount + 1
}

// IsExhausted returns true if max turns have been reached.
func (tm *TurnManager) IsExhausted() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.turnCount >= tm.maxTurns-1
}

// TurnLimitRemaining returns the number of turns remaining.
func (tm *TurnManager) TurnLimitRemaining() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	rem := tm.maxTurns - tm.turnCount - 1
	if rem < 0 {
		return 0
	}
	return rem
}

// Participants returns a copy of the participant list.
func (tm *TurnManager) Participants() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]string, len(tm.participants))
	copy(out, tm.participants)
	return out
}

// MaxTurns returns the configured maximum turns.
func (tm *TurnManager) MaxTurns() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.maxTurns
}
