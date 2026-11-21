package rpc

// 01-safety TEST GATE T5 (00T-gate.md): critical delivery ring (C6).
//
// The poll buffer trims drop-oldest at MaxEvents=100 regardless of topic
// (internal/rpc/proxy.go collector): under a progress flood the single
// turn.terminal / task.completed record is evicted before the client's
// next poll and the awaiting client hangs forever (F-NEW-3).
//
// C6: critical topics (turn.terminal, task.completed, task.failed,
// task.rejected, plan.completed, plan.rejected, plan.failed) live in a
// SEPARATE ring (cap 256) that the progress trim never touches; seq stays
// monotonic ACROSS rings (single nextSeq counter); dropped counts only
// non-critical loss.
//
// RED today: the terminal is trimmed out of the buffer by 300 progress
// events published after it.

import (
	"fmt"
	"testing"
	"time"
)

// TestCriticalRingNoTrim pins the load shape from the GUI rerun: one
// terminal buffered, then a 300-event progress flood, then poll. The
// terminal MUST still be deliverable.
func TestCriticalRingNoTrim(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("agent.progress", "turn.terminal")

	// The terminal lands FIRST (the turn finished), then the flood of
	// unrelated progress events fills the legacy 100-record buffer.
	f.publish("turn.terminal", "the-terminal")
	f.waitBuffered(subID, 1)
	for i := 0; i < 300; i++ {
		f.publish("agent.progress", fmt.Sprintf("progress-%03d", i))
	}
	// Wait until the flood has been fully processed by the collectors.
	deadline := time.Now().Add(10 * time.Second)
	var resp pollResp
	for time.Now().Before(deadline) {
		resp = f.poll(subID, 0)
		if resp.NextSeq >= 301 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if resp.NextSeq < 301 {
		t.Fatalf("flood never fully buffered: next_seq=%d, want >=301", resp.NextSeq)
	}

	var terminal *busEventRecord
	for i := range resp.Events {
		if resp.Events[i].Topic == "turn.terminal" {
			terminal = &resp.Events[i]
			break
		}
	}
	if terminal == nil {
		t.Fatalf("CURRENT DEFECT: turn.terminal was trimmed out of the poll buffer by the progress flood (drop-oldest MaxEvents=100); the awaiting client never learns the turn ended. dropped=%d", resp.Dropped)
	}

	// Seq monotonicity across rings: the merged poll page must be
	// strictly increasing (single nextSeq counter, C6 branch contract).
	var prev int64
	for _, e := range resp.Events {
		if e.Seq <= prev {
			t.Errorf("seq not monotonic across rings: %d after %d", e.Seq, prev)
		}
		prev = e.Seq
	}
	// The terminal keeps its ORIGINAL assignment seq (1): it was the
	// first record; the ring split must not renumber it.
	if terminal.Seq != 1 {
		t.Errorf("terminal seq = %d, want 1 (assignment order preserved across rings)", terminal.Seq)
	}
}

// TestCriticalRingDroppedCountsProgressOnly pins the accounting half of
// C6: overflow loss is reported in-band, and a delivered critical event
// is never counted as dropped.
func TestCriticalRingDroppedCountsProgressOnly(t *testing.T) {
	f := newBusStreamFixture(t)
	subID := f.subscribe("agent.progress", "task.completed")

	for i := 0; i < 250; i++ {
		f.publish("agent.progress", fmt.Sprintf("p-%03d", i))
	}
	f.publish("task.completed", "task-done")

	deadline := time.Now().Add(10 * time.Second)
	var resp pollResp
	for time.Now().Before(deadline) {
		resp = f.poll(subID, 0)
		if resp.NextSeq >= 251 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if resp.NextSeq < 251 {
		t.Fatalf("events never fully buffered: next_seq=%d", resp.NextSeq)
	}

	found := false
	for _, e := range resp.Events {
		if e.Topic == "task.completed" {
			found = true
		}
	}
	if !found {
		t.Errorf("CURRENT DEFECT: task.completed trimmed by progress overflow; critical topics are exempt from drop-oldest (C6)")
	}
	if resp.Dropped <= 0 {
		t.Errorf("dropped = %d; progress overflow must stay visible in-band (dropped>0), never silent", resp.Dropped)
	}
}
