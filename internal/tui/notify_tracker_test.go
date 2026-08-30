package tui

import (
	"testing"
)

func TestNotifySnapshot_UpdateAndPeek(t *testing.T) {
	n := NewNotifySnapshot()
	n.Update("emp-1", "build passed")
	text, empID, at := n.Peek()
	if text != "build passed" {
		t.Errorf("text = %q, want build passed", text)
	}
	if empID != "emp-1" {
		t.Errorf("empID = %q, want emp-1", empID)
	}
	if at.IsZero() {
		t.Error("at is zero")
	}
}

func TestNotifySnapshot_Take(t *testing.T) {
	n := NewNotifySnapshot()
	n.Update("emp-2", "task done")
	text, empID, at, ok := n.Take()
	if !ok {
		t.Error("Take() = false, want true")
	}
	if text != "task done" {
		t.Errorf("text = %q, want task done", text)
	}
	if empID != "emp-2" {
		t.Errorf("empID = %q, want emp-2", empID)
	}
	if at.IsZero() {
		t.Error("at is zero")
	}
	_, _, _, ok2 := n.Take()
	if ok2 {
		t.Error("second Take() = true, want false")
	}
}

func TestNotifySnapshot_Empty(t *testing.T) {
	n := NewNotifySnapshot()
	text, empID, at, ok := n.Take()
	if ok {
		t.Error("empty Take() = true, want false")
	}
	if text != "" || empID != "" || !at.IsZero() {
		t.Error("empty snapshot returned non-zero values")
	}
}
