//go:build e2e

// Gate leaf 00T — R2 admission control (T5, T6; Contract C3, leaf 02).
//
// T5: a prompt whose estimated input exceeds the model context is
// rejected pre-dispatch with ErrContextTooSmall and the fake upstream
// sees ZERO requests. T6: ContextLimit == 0 (the key absent) is an
// unknown limit — the request is ALLOWED through to upstream.
//
// RED today: on main the oversized prompt is dispatched to the upstream
// (no admission gate exists), so T5's upstream-counter and error
// assertions fail at runtime; the ErrContextTooSmall symbol itself is
// asserted in contracts_test.go (compile-RED until leaf 02 lands).
package sovereignparity

import (
	"strings"
	"testing"
	"time"
)

// admissionConfig: one provider whose model declares a tiny context
// limit, so the oversized prompt provably cannot fit.
func admissionConfig(t *testing.T) parityConfig {
	t.Helper()
	stub := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "provadm",
				stub:   stub,
				apiKey: "key-adm",
				models: []parityModel{
					{name: "model-tiny", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 512, maxOutput: 64},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provadm/model-tiny"}, timeout: 5, maxFails: 3},
		},
		chatAlias: "general",
	}
}

// TestAdmission_ContextTooSmallRejectsPreDispatch (T5): a prompt far over
// the 512-token context must be rejected before ANY upstream request.
// Gate contract C3: est + reserve(64) > context -> skip("context_too_small").
func TestAdmission_ContextTooSmallRejectsPreDispatch(t *testing.T) {
	cfg := admissionConfig(t)
	stub := cfg.providers[0].stub
	s := newParitySandbox(t, cfg)

	// ~4000-char prompt = ~1000 tokens by the rune/4 heuristic — far over
	// the 512-token context even before the 64-token output reserve.
	big := strings.Repeat("please summarize this totally ordinary prose sentence. ", 75)
	sessID := s.createSession(t, "admission-big")
	ack := s.submitChat(t, sessID, big)
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("oversized submit minted no turn_id: %+v", ack)
	}

	// The terminal event must NOT be a completed turn: either the gate
	// rejects pre-dispatch (leaf 02 landed) or the turn completes against
	// the upstream (RED — no gate exists today).
	sess, subID := subscribeTopic(t, s, "turn.terminal")
	defer sess.Close()
	term := waitTerminal(t, sess, subID, turnID, 120*time.Second)

	if term.Status == "completed" {
		t.Fatalf("R2 admission RED (expected): oversized prompt COMPLETED against the upstream — no pre-dispatch context gate exists. want ErrContextTooSmall rejection, got completed turn (reply %.80q)", term.Reply)
	}
	if stub.hitCount() != 0 {
		t.Fatalf("R2 admission: the fake upstream saw %d requests; a context-gate rejection must dispatch ZERO upstream requests", stub.hitCount())
	}
}

// TestAdmission_UnknownContextAllows (T6): a model with NO context_limit
// key has an unknown limit; the gate must allow the request through.
func TestAdmission_UnknownContextAllows(t *testing.T) {
	stub := newParityStub(t)
	cfg := parityConfig{
		providers: []parityProvider{
			{
				id:     "provunk",
				stub:   stub,
				apiKey: "key-unk",
				models: []parityModel{
					// ctxLimit 0 = the context_limit key is OMITTED.
					{name: "model-unknown", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 0, maxOutput: 64},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provunk/model-unknown"}, timeout: 5, maxFails: 3},
		},
		chatAlias: "general",
	}
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "admission-unknown")
	if reply := s.chatTurn(t, sessID, "hello unknown context", 120*time.Second); reply == "" {
		t.Fatal("unknown-context turn returned empty reply")
	}
	if stub.hitCount() == 0 {
		t.Fatal("T6: an unknown context limit must ALLOW the request; the fake upstream saw zero requests")
	}
	t.Logf("T6: unknown-context model served %d request(s)", stub.hitCount())
}
