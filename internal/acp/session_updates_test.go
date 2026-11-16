package acp

// RED gate tests for the ACP session updates + stopReason plumbing (plan
// docs/plans/20261115-inter-agent-coordination, branch 02-acp-emit, test
// leaf 00T tasks T4-T6). These reference the TARGET API only:
//   - Session.Send(ctx, text) (string, StopReason, error)  // C3 signature
//   - acp.StopReason (local string alias; acp must not import agent)
//   - onNotice handling plan / agent_thought_chunk / available_commands_update
//   - SetTurnStatePublisher nil-safe seam emitting running at prompt entry
//     and idle+stopReason at prompt exit
// They fail RED (wrong-arity Send compile error / missing event kinds /
// missing emission) until leaves 02-01 + 02-02 land. Tests ONLY — no
// implementation changes here.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T4 — Session.Send returns the ACP stopReason.
// ---------------------------------------------------------------------------

// TestACPPrompt_StopReason proves Send plumbs the wire stopReason through:
// a fake agent replies {"stopReason":"end_turn"} and Send returns it
// alongside the accumulated chat text. RED today: Send returns 2 values.
func TestACPPrompt_StopReason(t *testing.T) {
	s := startEcho(t, "echo")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	out, stop, err := s.Send(ctx, "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if out != "hello" {
		t.Fatalf("Send output = %q, want %q", out, "hello")
	}
	if stop != StopReason("end_turn") {
		t.Fatalf("Send stopReason = %q, want %q", stop, "end_turn")
	}
}

// TestACPSend_TurnEndEventKind pins the SessionEvent projection: a completed
// prompt emits Kind "turn_end" carrying the raw wire stopReason in Text.
func TestACPSend_TurnEndEventKind(t *testing.T) {
	s := startEcho(t, "echo")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if _, _, err := s.Send(ctx, "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var sawTurnEnd bool
	deadline := time.After(2 * time.Second)
	for !sawTurnEnd {
		select {
		case ev := <-s.Events():
			if ev.Kind == "turn_end" {
				sawTurnEnd = true
				if ev.Text != "end_turn" {
					t.Fatalf("turn_end event text = %q, want %q", ev.Text, "end_turn")
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for turn_end SessionEvent")
		}
	}
}

// ---------------------------------------------------------------------------
// T5 — onNotice handles plan / agent_thought_chunk / available_commands_update.
// ---------------------------------------------------------------------------

// TestACPSession_UpdateKinds drives onNotice with scripted session/update
// notifications and asserts the projected SessionEvent kind per variant.
// RED today: onNotice handles only agent_message_chunk + tool_call kinds.
func TestACPSession_UpdateKinds(t *testing.T) {
	s := startEcho(t, "echo")

	tests := []struct {
		name         string
		sessionName  string // sessionUpdate discriminator
		paramsUpdate map[string]any
		wantKind     string
		wantTextSub  string // substring expected in the projected Text ("" = no check)
	}{
		{
			name:        "plan emits plan event",
			sessionName: "plan",
			paramsUpdate: map[string]any{
				"sessionUpdate": "plan",
				"title":         "Ship the fix",
			},
			wantKind:    "plan",
			wantTextSub: "Ship the fix",
		},
		{
			name:        "agent_thought_chunk emits thought event",
			sessionName: "agent_thought_chunk",
			paramsUpdate: map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]string{"type": "text", "text": "thinking hard about it"},
			},
			wantKind:    "thought",
			wantTextSub: "thinking hard about it",
		},
		{
			name:        "available_commands_update emits commands event",
			sessionName: "available_commands_update",
			paramsUpdate: map[string]any{
				"sessionUpdate": "available_commands_update",
				"commands": []map[string]string{
					{"name": "build", "description": "build the project"},
				},
			},
			wantKind:    "commands",
			wantTextSub: "build",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params, err := json.Marshal(map[string]any{
				"sessionId": "sess-fake",
				"update":    tc.paramsUpdate,
			})
			if err != nil {
				t.Fatalf("marshal update params: %v", err)
			}
			// onNotice is the notification seam: drive it directly so the
			// assertion pins the projection, not the subprocess transport.
			s.onNotice(Notification{
				JSONRPC: "2.0",
				Method:  MethodSessionUpdate,
				Params:  params,
			})

			deadline := time.After(2 * time.Second)
			for {
				select {
				case ev := <-s.Events():
					// Drain queued events until the target kind shows up
					// (or the window closes).
					if ev.Kind == tc.wantKind {
						if tc.wantTextSub != "" && !strings.Contains(ev.Text, tc.wantTextSub) {
							t.Fatalf("%s event text = %q, want it to contain %q", tc.wantKind, ev.Text, tc.wantTextSub)
						}
						return
					}
				case <-deadline:
					t.Fatalf("timed out waiting for SessionEvent kind %q (only agent_message_chunk/tool_call are handled today)", tc.wantKind)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T6 — ACP turn boundaries emit turn state via the publisher seam.
// ---------------------------------------------------------------------------

// TestACPTurn_EmitsState proves a Send publishes TurnStateEvent-shaped
// callbacks: running at prompt entry, idle+stopReason at prompt exit.
// The seam is the nil-safe SetTurnStatePublisher; a session with no
// publisher wired must stay byte-identical (nil-safe no-op).
func TestACPTurn_EmitsState(t *testing.T) {
	t.Run("unwired publisher is nil-safe", func(t *testing.T) {
		s := startEcho(t, "echo")
		// No SetTurnStatePublisher call: Send must complete normally.
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, _, err := s.Send(ctx, "hello"); err != nil {
			t.Fatalf("Send with no publisher wired: %v", err)
		}
	})

	t.Run("send emits running then idle with stop reason", func(t *testing.T) {
		s := startEcho(t, "echo")

		type call struct {
			agentID string
			state   string
			reason  string
			phase   string
		}
		calls := make(chan call, 8)
		s.SetTurnStatePublisher(func(agentID, state, stopReason, phase string) {
			select {
			case calls <- call{agentID: agentID, state: state, reason: stopReason, phase: phase}:
			default:
			}
		})

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, _, err := s.Send(ctx, "hello"); err != nil {
			t.Fatalf("Send: %v", err)
		}

		var got []call
		deadline := time.After(2 * time.Second)
	collect:
		for len(got) < 2 {
			select {
			case c := <-calls:
				got = append(got, c)
			case <-deadline:
				break collect
			}
		}

		if len(got) < 2 {
			t.Fatalf("got %d turn-state callbacks, want 2 (running at entry, idle at exit)", len(got))
		}
		if got[0].state != "running" {
			t.Errorf("first callback state = %q, want %q", got[0].state, "running")
		}
		if got[0].agentID != s.AgentID() {
			t.Errorf("first callback agent id = %q, want %q", got[0].agentID, s.AgentID())
		}
		last := got[len(got)-1]
		if last.state != "idle" {
			t.Errorf("final callback state = %q, want %q", last.state, "idle")
		}
		if last.reason != "end_turn" {
			t.Errorf("final callback stop reason = %q, want %q", last.reason, "end_turn")
		}
	})
}
