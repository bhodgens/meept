package tui

import (
	"strings"
	"testing"
)

// matchWildcardSeg mirrors internal/bus matchWildcard (equal segment counts).
func matchWildcardSeg(pattern, topic string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == topic
	}
	pp := strings.Split(pattern, ".")
	tp := strings.Split(topic, ".")
	if len(pp) != len(tp) {
		return false
	}
	for i, part := range pp {
		if part != "*" && part != tp[i] {
			return false
		}
	}
	return true
}

// TestTopicPatternsAreNonOverlapping pins the H9 invariant (bughunt
// 2026-10-05): no topic may match MORE THAN ONE pattern in
// DefaultEventStreamConfig's topic list. bus.subscribe creates one collector
// per matching pattern and bus.poll returns every buffered record without
// dedupe, so an overlapping pair delivers every matched event twice — once to
// the activity feed per duplicate. Non-vacuous by construction: the pre-H9
// list (with agent.event.* AND agent.*.* both present) fails this test, since
// agent.progress.synthesized matched both.
//
// The topic set is the repo's real published agent.* / queue.* / task.* etc.
// topics plus the wildcard-shaped extremes, so a future topic that lands in a
// gap between the patterns is caught here rather than in production.
func TestTopicPatternsAreNonOverlapping(t *testing.T) {
	patterns := DefaultEventStreamConfig().Topics
	if len(patterns) < 2 {
		t.Fatal("event stream config must carry multiple patterns for this guard to mean anything")
	}

	// The historical offender: these five topics matched two patterns each
	// before H9. Keep them in the probe set so a regression is caught.
	probeTopics := []string{
		"agent.progress.synthesized",
		"agent.event.turn_start",
		"agent.event.turn_end",
		"agent.event.tool_execution_start",
		"agent.event.tool_execution_end",
		"agent.lifecycle.started",
		"agent.lifecycle.ended",
		"agent.queue.steer.added",
		"agent.queue.steer.injected",
		"agent.queue.persisted",
		"agent.state.changed",
		"agent.action",
		"agent.model_escalated",
		"task.progress",
		"task.completed",
		"queue.job.completed",
		"turn.terminal",
		"chat_message",
	}

	for _, topic := range probeTopics {
		matches := 0
		var matched []string
		for _, p := range patterns {
			if matchWildcardSeg(p, topic) {
				matches++
				matched = append(matched, p)
			}
		}
		if matches > 1 {
			t.Errorf("topic %q matches %d patterns (%v) — every one of them delivers a duplicate copy to the TUI", topic, matches, matched)
		}
	}

	// Pattern-level pairwise check: two patterns whose wildcard shapes can
	// both match a common topic form. Two concrete-segment patterns overlap
	// iff one subsumes the other at equal segment count; enumerate the
	// cross-product by testing a synthetic topic where each pattern's
	// literals are kept and the other's wildcards filled with "x".
	for i := range patterns {
		for j := i + 1; j < len(patterns); j++ {
			a, b := patterns[i], patterns[j]
			if a == b {
				t.Errorf("duplicate pattern %q in the topic list", a)
				continue
			}
			synth := overlapWitness(a, b)
			if synth == "" {
				continue
			}
			if matchWildcardSeg(a, synth) && matchWildcardSeg(b, synth) {
				t.Errorf("patterns %q and %q overlap: both match %q", a, b, synth)
			}
		}
	}
}

// overlapWitness builds a concrete topic that both patterns would match, or
// "" when the segment counts differ (matchWildcard then can never match both).
func overlapWitness(a, b string) string {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	if len(ap) != len(bp) {
		return ""
	}
	out := make([]string, len(ap))
	for i := range ap {
		switch {
		case ap[i] != "*":
			out[i] = ap[i]
		case bp[i] != "*":
			out[i] = bp[i]
		default:
			out[i] = "x"
		}
	}
	return strings.Join(out, ".")
}

// Non-vacuity proof for TestTopicPatternsAreNonOverlapping: the PRE-H9 topic
// list (agent.*.* coexisting with agent.event.* / agent.progress.*) MUST trip
// the guard. If this test ever fails, the overlap detector is vacuous and the
// main pin proves nothing.
func TestTopicPatternsOverlapNonVacuity(t *testing.T) {
	preH9 := []string{"agent.*", "agent.*.*", "agent.*.*.*", "agent.event.*", "agent.progress.*"}
	found := false
	for i := range preH9 {
		for j := i + 1; j < len(preH9); j++ {
			synth := overlapWitness(preH9[i], preH9[j])
			if synth == "" {
				continue
			}
			if matchWildcardSeg(preH9[i], synth) && matchWildcardSeg(preH9[j], synth) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the pre-H9 pattern list no longer trips the overlap detector — the guard is vacuous")
	}
}
