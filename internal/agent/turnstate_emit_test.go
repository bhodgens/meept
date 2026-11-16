package agent

// RED gate tests for the delegate/pair turn-state emitters (plan
// docs/plans/20261115-inter-agent-coordination, branch 02-acp-emit, test
// leaf 00T tasks T1-T3). These reference the TARGET API only:
//   - DelegateResult.StopReason (C2)
//   - resultFromRecord mapping (completed->end_turn, interrupted->cancelled,
//     failed->error, refusal classified at settle time -> refusal)
//   - turn-state emission on the delegate publisher seam ("agent.turn.state")
//   - pair phase transitions publishing TurnStateEvent on TopicTurnState
// They fail RED (compile error on the missing StopReason field / assertion
// failure on missing emission) until leaf 02-01 lands. Tests ONLY — no
// implementation changes here.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/models"
)

// ---------------------------------------------------------------------------
// T1 — DelegateResult carries StopReason; resultFromRecord mapping table.
// ---------------------------------------------------------------------------

// TestResultFromRecord_StopReason pins the C2 mapping table: a settled
// ChildRecord maps to the DelegateResult StopReason per the contract.
// The refusal row is NOT string-matched from rec.Err — it is exercised
// end-to-end through the settle path in
// TestDelegate_EmitsTurnState/refusal (the typed *llm.RefusalError is only
// in scope at settle time, where classification happens).
func TestResultFromRecord_StopReason(t *testing.T) {
	mgr := NewDelegateManager(nil, nil, nil)

	tests := []struct {
		name string
		rec  ChildRecord
		want StopReason
	}{
		{
			name: "completed with no error maps to end_turn",
			rec:  ChildRecord{AgentID: "a", ChildID: "c1", State: ChildStateCompleted, Result: "done"},
			want: StopReasonEndTurn,
		},
		{
			name: "interrupted maps to cancelled",
			rec:  ChildRecord{AgentID: "a", ChildID: "c2", State: ChildStateInterrupted, Err: "cancelled"},
			want: StopReasonCancelled,
		},
		{
			name: "failed maps to error",
			rec:  ChildRecord{AgentID: "a", ChildID: "c3", State: ChildStateFailed, Err: "boom"},
			want: StopReasonError,
		},
		{
			name: "pending carries no stop reason",
			rec:  ChildRecord{AgentID: "a", ChildID: "c4", State: ChildStatePending},
			want: "",
		},
		{
			name: "running carries no stop reason",
			rec:  ChildRecord{AgentID: "a", ChildID: "c5", State: ChildStateRunning},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := mgr.resultFromRecord(tc.rec)
			if res.StopReason != tc.want {
				t.Fatalf("resultFromRecord(%s).StopReason = %q, want %q", tc.rec.State, res.StopReason, tc.want)
			}
			// Success classification stays tied to the completed+no-error rule.
			wantSuccess := tc.rec.State == ChildStateCompleted && tc.rec.Err == ""
			if res.Success != wantSuccess {
				t.Fatalf("resultFromRecord(%s).Success = %v, want %v", tc.rec.State, res.Success, wantSuccess)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T2 — delegate spawn/settle emit turn state on the publisher seam.
// ---------------------------------------------------------------------------

// turnStateCapture collects events published on "agent.turn.state" through
// the DelegateManager publisher seam (func(topic string, payload any)).
type turnStateCapture struct {
	events chan TurnStateEvent
}

func newTurnStateCapture() *turnStateCapture {
	return &turnStateCapture{events: make(chan TurnStateEvent, 32)}
}

func (c *turnStateCapture) publisher(topic string, payload any) {
	if topic != "agent.turn.state" {
		return
	}
	ev, ok := payload.(TurnStateEvent)
	if !ok {
		return
	}
	select {
	case c.events <- ev:
	default:
	}
}

// waitForTurnStateEvents drains up to n events or until the deadline.
func waitForTurnStateEvents(t *testing.T, ch <-chan TurnStateEvent, n int, d time.Duration) []TurnStateEvent {
	t.Helper()
	var got []TurnStateEvent
	deadline := time.After(d)
	for len(got) < n {
		select {
		case ev := <-ch:
			got = append(got, ev)
		case <-deadline:
			return got
		}
	}
	return got
}

// TestDelegate_EmitsTurnState proves a delegated child publishes
// TurnStateEvent running at spawn and idle (with the classified stop
// reason) at settle, through the SAME publisher seam as agent.delegate.result.
func TestDelegate_EmitsTurnState(t *testing.T) {
	t.Run("spawn publishes running; settle publishes idle+end_turn", func(t *testing.T) {
		reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
		if err := reg.RegisterSpec(&AgentSpec{ID: "delegate-emit-child", Name: "Child", Enabled: true}); err != nil {
			t.Fatalf("RegisterSpec: %v", err)
		}

		cap := newTurnStateCapture()
		mgr := NewDelegateManager(reg, cap.publisher, nil)
		mgr.SetRunFunc(func(ctx context.Context, agentID, message, conversationID, modelRef string) (string, error) {
			return "child done", nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		res, err := mgr.Spawn(ctx, DelegateRequest{AgentID: "delegate-emit-child", Message: "do the thing"}, "sess-parent")
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		joined, err := mgr.Join(ctx, "sess-parent", res.ChildID, 5*time.Second)
		if err != nil {
			t.Fatalf("Join: %v", err)
		}
		if !joined.Success || joined.Status != string(ChildStateCompleted) {
			t.Fatalf("joined result = %+v, want completed success", joined)
		}

		events := waitForTurnStateEvents(t, cap.events, 2, 2*time.Second)
		if len(events) < 2 {
			t.Fatalf("got %d turn-state events, want 2 (running at spawn, idle at settle)", len(events))
		}

		var running, idle *TurnStateEvent
		for i := range events {
			switch events[i].State {
			case TurnStateRunning:
				running = &events[i]
			case TurnStateIdle:
				idle = &events[i]
			}
		}
		if running == nil {
			t.Fatalf("no running event in %+v", events)
		}
		if running.AgentID != "delegate-emit-child" || running.ChildID != res.ChildID {
			t.Errorf("running event identity = agent %q child %q, want agent %q child %q",
				running.AgentID, running.ChildID, "delegate-emit-child", res.ChildID)
		}
		if running.Phase != "spawn" {
			t.Errorf("running event phase = %q, want %q", running.Phase, "spawn")
		}
		if idle == nil {
			t.Fatalf("no idle event in %+v", events)
		}
		if idle.Phase != "settled" {
			t.Errorf("idle event phase = %q, want %q", idle.Phase, "settled")
		}
		if idle.StopReason != StopReasonEndTurn {
			t.Errorf("idle event stop reason = %q, want %q", idle.StopReason, StopReasonEndTurn)
		}
		if len(events) != 2 {
			t.Errorf("got %d turn-state events, want exactly 2", len(events))
		}
	})

	// The refusal row of the C2 mapping table: classification happens where
	// the typed *llm.RefusalError is in scope (settle time), never by
	// matching the stored error string. Proven end-to-end: a run seam that
	// returns a typed refusal must settle into a result (and a terminal
	// turn-state event) carrying StopReasonRefusal.
	t.Run("typed refusal error classifies to stop_reason refusal", func(t *testing.T) {
		reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger()})
		if err := reg.RegisterSpec(&AgentSpec{ID: "delegate-emit-refuser", Name: "Refuser", Enabled: true}); err != nil {
			t.Fatalf("RegisterSpec: %v", err)
		}

		cap := newTurnStateCapture()
		mgr := NewDelegateManager(reg, cap.publisher, nil)
		mgr.SetRunFunc(func(ctx context.Context, agentID, message, conversationID, modelRef string) (string, error) {
			return "", &llm.RefusalError{
				ProviderID:   "prov",
				ModelID:      "model-x",
				Source:       "finish_reason",
				FinishReason: "refusal",
			}
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		res, err := mgr.Spawn(ctx, DelegateRequest{AgentID: "delegate-emit-refuser", Message: "do the thing"}, "sess-parent")
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		joined, err := mgr.Join(ctx, "sess-parent", res.ChildID, 5*time.Second)
		if err != nil {
			t.Fatalf("Join: %v", err)
		}
		if joined.Success {
			t.Fatal("refused child joined as success")
		}
		if joined.StopReason != StopReasonRefusal {
			t.Fatalf("joined.StopReason = %q, want %q (settle-time typed-error classification)", joined.StopReason, StopReasonRefusal)
		}

		events := waitForTurnStateEvents(t, cap.events, 2, 2*time.Second)
		var idle *TurnStateEvent
		for i := range events {
			if events[i].State == TurnStateIdle || events[i].State == TurnStateRequiresAction {
				idle = &events[i]
			}
		}
		if idle == nil {
			t.Fatalf("no terminal turn-state event in %+v", events)
		}
		if idle.StopReason != StopReasonRefusal {
			t.Errorf("terminal event stop reason = %q, want %q", idle.StopReason, StopReasonRefusal)
		}
		// Q-02: refusal needs input, so requires_action is the recommended
		// terminal state — idle is tolerated. Both must carry the refusal.
		if idle.State != TurnStateRequiresAction && idle.State != TurnStateIdle {
			t.Errorf("terminal event state = %q, want requires_action (or idle)", idle.State)
		}
	})
}

// ---------------------------------------------------------------------------
// T3 — pair phase transitions emit TurnStateEvent per phase.
// ---------------------------------------------------------------------------

// TestPair_EmitsTurnState proves the PairOrchestrator publishes a
// TurnStateEvent on TopicTurnState at every phase transition:
// actor_turn/reviewer_turn -> running, completed -> idle, failed -> idle
// with StopReasonError.
func TestPair_EmitsTurnState(t *testing.T) {
	t.Run("completed path emits running running idle", func(t *testing.T) {
		msgBus := bus.New(nil, slogDiscardLogger())
		defer msgBus.Close()

		// Reviewer's canned reply starts with APPROVED so the conversation
		// converges after one actor/reviewer cycle (phase "completed").
		chatter := newMockChatter(
			&llm.Response{Content: "actor work product"},
			&llm.Response{Content: "APPROVED: ship it"},
			&llm.Response{Content: "APPROVED: ship it"},
			&llm.Response{Content: "APPROVED: ship it"},
		)
		reg := NewAgentRegistry(RegistryConfig{Logger: silentLogger(), SharedChatter: chatter})
		for _, id := range []string{"pair-actor", "pair-reviewer"} {
			if err := reg.RegisterSpec(&AgentSpec{ID: id, Name: id, Enabled: true}); err != nil {
				t.Fatalf("RegisterSpec(%s): %v", id, err)
			}
		}

		evCh := make(chan TurnStateEvent, 16)
		sub := bus.SubscribeT(msgBus, "pair-turnstate-test", TopicTurnState, func(ev TurnStateEvent) {
			select {
			case evCh <- ev:
			default:
			}
		})
		defer msgBus.Unsubscribe(sub)

		// Diagnostic: collect the pair's own turn publications so a rig
		// failure names the phase that actually ran.
		turnCh := make(chan *models.BusMessage, 16)
		turnSub := msgBus.Subscribe("pair-turns-diag", PairTopic("pair-emit-completed"))
		go func() {
			for m := range turnSub.Channel {
				select {
				case turnCh <- m:
				default:
				}
			}
		}()
		defer msgBus.Unsubscribe(turnSub)

		po := NewPairOrchestrator(PairOrchestratorDeps{
			Registry: reg,
			Bus:      msgBus,
			Logger:   slogDiscardLogger(),
		})
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := po.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			_ = po.Stop(stopCtx)
		}()

		time.Sleep(10 * time.Millisecond)

		req := PairStartRequest{
			SessionID:     "pair-emit-completed",
			ActorID:       "pair-actor",
			ReviewerID:    "pair-reviewer",
			InitialPrompt: "write the thing",
			MaxTurns:      1,
		}
		payload, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal pair start: %v", err)
		}
		msgBus.Publish(TopicPairStart, &models.BusMessage{
			ID:        "pair-emit-completed-1",
			Type:      models.MessageTypeRequest,
			Topic:     TopicPairStart,
			Source:    "test",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})

		events := waitForTurnStateEvents(t, evCh, 3, 8*time.Second)
		if len(events) < 3 {
			var turns []*models.BusMessage
			for {
				select {
				case m := <-turnCh:
					turns = append(turns, m)
					continue
				default:
				}
				break
			}
			t.Fatalf("got %d turn-state events, want 3 (actor_turn running, reviewer_turn running, completed idle); pair turns observed: %d", len(events), len(turns))
		}

		type phaseWant struct {
			phase string
			state AgentTurnState
		}
		wants := []phaseWant{
			{"actor_turn", TurnStateRunning},
			{"reviewer_turn", TurnStateRunning},
			{"completed", TurnStateIdle},
		}
		for i, w := range wants {
			ev := events[i]
			if ev.Phase != w.phase {
				t.Errorf("event[%d] phase = %q, want %q", i, ev.Phase, w.phase)
			}
			if ev.State != w.state {
				t.Errorf("event[%d] (%s) state = %q, want %q", i, ev.Phase, ev.State, w.state)
			}
			if ev.SessionID != "pair-emit-completed" {
				t.Errorf("event[%d] session_id = %q, want %q", i, ev.SessionID, "pair-emit-completed")
			}
		}
		// The running events must name the agent whose turn it is.
		if events[0].AgentID != "pair-actor" {
			t.Errorf("actor_turn event agent_id = %q, want %q", events[0].AgentID, "pair-actor")
		}
		if events[1].AgentID != "pair-reviewer" {
			t.Errorf("reviewer_turn event agent_id = %q, want %q", events[1].AgentID, "pair-reviewer")
		}
	})

	t.Run("failed path emits running then idle with stop_reason error", func(t *testing.T) {
		msgBus := bus.New(nil, slogDiscardLogger())
		defer msgBus.Close()

		// Nil registry: runAgent fails immediately at the actor phase.
		po := NewPairOrchestrator(PairOrchestratorDeps{
			Registry: nil,
			Bus:      msgBus,
			Logger:   slogDiscardLogger(),
		})

		evCh := make(chan TurnStateEvent, 16)
		sub := bus.SubscribeT(msgBus, "pair-turnstate-fail-test", TopicTurnState, func(ev TurnStateEvent) {
			select {
			case evCh <- ev:
			default:
			}
		})
		defer msgBus.Unsubscribe(sub)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := po.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			_ = po.Stop(stopCtx)
		}()

		time.Sleep(10 * time.Millisecond)

		req := PairStartRequest{
			SessionID:     "pair-emit-failed",
			ActorID:       "pair-actor",
			ReviewerID:    "pair-reviewer",
			InitialPrompt: "write the thing",
			MaxTurns:      1,
		}
		payload, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal pair start: %v", err)
		}
		msgBus.Publish(TopicPairStart, &models.BusMessage{
			ID:        "pair-emit-failed-1",
			Type:      models.MessageTypeRequest,
			Topic:     TopicPairStart,
			Source:    "test",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		})

		events := waitForTurnStateEvents(t, evCh, 2, 5*time.Second)
		if len(events) < 2 {
			t.Fatalf("got %d turn-state events, want 2 (actor_turn running, failed idle+error)", len(events))
		}
		if events[0].Phase != "actor_turn" || events[0].State != TurnStateRunning {
			t.Errorf("event[0] = phase %q state %q, want actor_turn/running", events[0].Phase, events[0].State)
		}
		last := events[len(events)-1]
		if last.Phase != "failed" {
			t.Errorf("terminal event phase = %q, want %q", last.Phase, "failed")
		}
		if last.State != TurnStateIdle {
			t.Errorf("terminal event state = %q, want %q", last.State, TurnStateIdle)
		}
		if last.StopReason != StopReasonError {
			t.Errorf("terminal event stop reason = %q, want %q", last.StopReason, StopReasonError)
		}
	})
}
