package kanban

import (
	"fmt"
	"time"
)

type Board struct {
	ID   string
	Name string
}

type Column struct {
	ID       string
	BoardID  string
	Name     string
	Position int
	WIP      int // WIP 0 = no limit
}

type WorkRef struct {
	Kind string // Kind: step|goal|job|ticket|plan
	ID   string
}

type Card struct {
	ID         string
	BoardID    string
	ColumnID   string
	Title      string
	Ref        WorkRef
	Labels     []string
	GoalID     string
	EmployeeID string
	PlanID     string
	SessionID  string
	TriggerRef string // provenance, may be ""
	Order      int    // manual ordering within column
}

type StateEvent struct {
	Kind   string    `json:"kind"`
	ID     string    `json:"id"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Reason string    `json:"reason"`
	TS     time.Time `json:"ts"`
}

type CardMoved struct {
	CardID       string
	FromColumnID string
	ToColumnID   string
	Actor        string
	LeaseBlocked bool
}

type ErrLeaseHeld struct {
	Holder string
	Since  time.Time
}

func (e ErrLeaseHeld) Error() string {
	return fmt.Sprintf("lease held by %s since %s", e.Holder, e.Since.Format(time.RFC3339))
}

type ErrUnknownCard struct {
	ID string
}

func (e ErrUnknownCard) Error() string {
	return fmt.Sprintf("unknown card: %s", e.ID)
}

type ErrWIPFull struct {
	ColumnID string
	Limit    int
}

func (e ErrWIPFull) Error() string {
	return fmt.Sprintf("WIP limit reached for column %s: %d", e.ColumnID, e.Limit)
}
