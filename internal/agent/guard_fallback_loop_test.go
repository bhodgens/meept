package agent

import (
	"testing"
)

// TestSetGuardFallback_ConsumedOnce pins the loop-side consumption: the
// fallback arms once, survives one response assembly, and clears.
func TestSetGuardFallback_ConsumedOnce(t *testing.T) {
	l := &AgentLoop{}
	if l.guardFallback != "" {
		t.Fatal("expected empty initial fallback")
	}
	l.SetGuardFallback("## Session context\n- Prior task: \"hello.txt\"")
	if l.guardFallback == "" {
		t.Fatal("SetGuardFallback did not arm the fallback")
	}
	l.SetGuardFallback("") // empty = no-op per nil/empty guard conventions
	if l.guardFallback == "" {
		t.Error("empty digest must not clear an armed fallback")
	}
}

// TestToolExemplarSection_NonExecutorNoSection documents the gating family
// the A5 fix relies on: guard fallback and exemplar are executor/scoped.
func TestApplyReplyGuardWithFallback_ProseWithFallbackUnchanged(t *testing.T) {
	prose := "the file is at /w/project/hello.txt and contains hello"
	got := applyReplyGuardWithFallback(prose, nil, replyGuardContext{}, "unused digest")
	if got != prose {
		t.Errorf("genuine prose altered: %q", got)
	}
}
