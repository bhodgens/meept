package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

// TestOneShotReturnsQuestionsNotStall pins leaf 04 one-shot honesty: a
// one-shot `meept chat` turn whose task pauses on a design interview
// gets the interview QUESTIONS as the turn.terminal reply (handler_case
// interview_pause, status completed) instead of waiting out the CLI
// liveness window and exiting with "turn stalled".
func TestOneShotReturnsQuestionsNotStall(t *testing.T) {
	questions := "I need a few answers before I can plan this task:\n\n1. which storage engine?\n\nReply to continue with your answers."
	var out bytes.Buffer
	f := newFakeAsyncClient(terminalEvent("turn-123", "completed", questions, "", 900), nil)

	res, err := submitAndAwait(context.Background(), f, "build a report pipeline", "sess-1", testChatOpts(&out))
	if err != nil {
		t.Fatalf("submitAndAwait returned error (one-shot must not stall): %v", err)
	}
	if !strings.Contains(res.Reply, "which storage engine?") {
		t.Errorf("reply %q missing the interview questions", res.Reply)
	}
	if !strings.Contains(res.Reply, "Reply to continue") {
		t.Errorf("reply %q missing the reply-to-continue note", res.Reply)
	}
	if res.Error != "" {
		t.Errorf("error = %q, want empty", res.Error)
	}
}

// TestOneShotStillStallsWithoutTerminal pins the control: when NO
// terminal event ever arrives, the liveness stall error still fires —
// the questions path is additive, it never disables stall detection.
func TestOneShotStillStallsWithoutTerminal(t *testing.T) {
	var out bytes.Buffer
	f := newFakeAsyncClient(nil, nil) // polls return no events, forever

	opts := testChatOpts(&out)
	opts.Liveness = 50 * time.Millisecond
	opts.PollInterval = 10 * time.Millisecond

	_, err := submitAndAwait(context.Background(), f, "do a thing", "sess-1", opts)
	if err == nil || !strings.Contains(err.Error(), "turn stalled") {
		t.Fatalf("err = %v, want turn stalled", err)
	}
}
