package tui

import (
	"sync"
	"time"
)

type NotifySnapshot struct {
	mu        sync.RWMutex
	text      string
	at        time.Time
	empID     string
	discarded bool
}

func NewNotifySnapshot() *NotifySnapshot {
	return &NotifySnapshot{discarded: true}
}

func (n *NotifySnapshot) Update(empID, text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.text = text
	n.at = time.Now().UTC()
	n.empID = empID
	n.discarded = false
}

func (n *NotifySnapshot) Peek() (text, empID string, at time.Time) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.text, n.empID, n.at
}

func (n *NotifySnapshot) Take() (text, empID string, at time.Time, ok bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.discarded {
		return "", "", time.Time{}, false
	}
	n.discarded = true
	return n.text, n.empID, n.at, true
}
