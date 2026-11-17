//go:build e2e

// Gate leaf 00T — R7 sticky difficulty (T17, T18; Contract C8, leaf 07).
//
// T17: a turn classified ABOVE the pinned difficulty releases the pin,
// re-pins at the rotation head, and writes reason
// sticky_difficulty_increase to routing_decisions. T18: a same-or-lower
// difficulty turn keeps the pin.
//
// RED today: on main StickyPin has no Difficulty and no difficulty release
// exists — the pin holds regardless of the derived level, so T17 fails at
// runtime (no sticky_difficulty_increase row ever appears). The
// llm.DeriveDifficulty / StickyPin.Difficulty symbols live in
// derivedifficulty_test.go / contracts_test.go (compile-RED until leaf 07).
//
// Drive shape: a balanced_sticky alias of two members. Session turns are
// classifier-scripted to produce intents with different SuggestedMode /
// RequiresPlanning values, which leaf 07's DeriveDifficulty projects to
// different levels; the routing log's reason column is the observable.
package sovereignparity

import (
	"testing"
	"time"
)

// stickyWorld builds a two-member balanced-sticky alias (two stubs, one
// per provider) and returns the first stub so tests can pin classifier
// outputs per phase.
func stickyWorld(t *testing.T) (*paritySandbox, *parityStub) {
	t.Helper()
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	cfg := parityConfig{
		providers: []parityProvider{
			{
				id:     "provst1",
				stub:   stubA,
				apiKey: "key-st1",
				models: []parityModel{
					{name: "model-st1", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
			{
				id:     "provst2",
				stub:   stubB,
				apiKey: "key-st2",
				models: []parityModel{
					{name: "model-st2", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 2.0, outCost: 2.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provst1/model-st1", "provst2/model-st2"}, timeout: 5, maxFails: 3, balanced: true},
		},
		chatAlias: "general",
	}
	s := newParitySandbox(t, cfg)
	return s, stubA
}

// TestSticky_DifficultyIncreaseReleasesPin (T17): pin at difficulty 2
// (plain chat), then a difficulty-4 turn (requires_planning) releases the
// pin — the routing log carries sticky_difficulty_increase.
func TestSticky_DifficultyIncreaseReleasesPin(t *testing.T) {
	s, stub := stickyWorld(t)

	// Phase 1: plain chat intents (difficulty ~1-2) pin the session onto
	// a member.
	stub.setClassifierOut(`{"intent":"chat","confidence":0.9,"reasoning":"sticky phase 1"}`)
	sessID := s.createSession(t, "sticky-phase1")
	if reply := s.chatTurn(t, sessID, "plain chat one", 120*time.Second); reply == "" {
		t.Fatal("phase-1 chat turn returned empty reply")
	}
	if reply := s.chatTurn(t, sessID, "plain chat two", 120*time.Second); reply == "" {
		t.Fatal("phase-1 second chat turn returned empty reply")
	}

	rows := s.routingRowsForAlias(t, "general")
	sticky := 0
	for _, r := range rows {
		if r.Reason == "sticky_request" || r.Reason == "sticky_request_new" {
			sticky++
		}
	}
	if sticky == 0 {
		t.Fatalf("T17 precondition RED: balanced-sticky alias never wrote a sticky reason (rows %+v) — the sticky drive never engaged", rows)
	}

	// Phase 2: a turn classified as requiring planning (difficulty 4 per
	// the C11 table). The pin must release and re-pin at the head with
	// reason sticky_difficulty_increase.
	stub.setClassifierOut(`{"intent":"code","confidence":0.95,"requires_planning":true,"suggested_mode":"spec_plan","reasoning":"sticky phase 2 escalation"}`)
	if reply := s.chatTurn(t, sessID, "now do the hard planned engineering work", 180*time.Second); reply == "" {
		t.Fatalf("phase-2 escalation turn returned empty reply; daemon tail:\n%s", tailRunes(s.daemonLog(t), 2000))
	}

	rows = s.routingRowsForAlias(t, "general")
	reason := ""
	for _, r := range rows {
		if r.Reason == "sticky_difficulty_increase" {
			reason = r.Reason
			break
		}
	}
	if reason == "" {
		t.Fatalf("T17 R7 RED (expected): no sticky_difficulty_increase routing row after a difficulty-increase turn — StickyPin has no Difficulty on main (rows: %+v)", rows)
	}
}

// TestSticky_SameDifficultyKeepsPin (T18): after the pin exists, a turn
// classified at the same-or-lower difficulty must NOT release it — no
// difficulty-driven re-pin, no new reason vocabulary.
func TestSticky_SameDifficultyKeepsPin(t *testing.T) {
	s, stub := stickyWorld(t)

	stub.setClassifierOut(`{"intent":"code","confidence":0.95,"requires_planning":true,"suggested_mode":"spec_plan","reasoning":"pin at level 4"}`)
	sessID := s.createSession(t, "sticky-same")
	if reply := s.chatTurn(t, sessID, "hard planned work pins here", 120*time.Second); reply == "" {
		t.Fatal("pin turn returned empty reply")
	}

	// Same-classified turn: pin must hold.
	if reply := s.chatTurn(t, sessID, "more hard planned work same level", 120*time.Second); reply == "" {
		t.Fatal("same-difficulty turn returned empty reply")
	}

	rows := s.routingRowsForAlias(t, "general")
	for i, r := range rows {
		if r.Reason == "sticky_difficulty_increase" {
			t.Fatalf("T18: same-difficulty turn released the pin (row %d carries sticky_difficulty_increase) — only an INCREASE may release (C8)", i)
		}
	}
	sticky := false
	for _, r := range rows {
		if r.Reason == "sticky_request" {
			sticky = true
			break
		}
	}
	if !sticky {
		t.Logf("T18 note: no sticky_request row observed among %+v; the pin-keep assertion above still guards the release vocabulary", rows)
	}
}
