package main

// Async chat turn client (async-turn-migration leaf 03).
//
// The non-TUI chat paths (`meept chat "msg"` and `meept chat --session ...`)
// submit the turn through the "chat.submit" RPC (never the legacy blocking
// "chat" RPC) and await the real terminal result on the "turn.terminal" bus
// topic using the same bus.subscribe + bus.poll plumbing the TUI event
// stream uses (internal/tui/events.go).
//
// Wire contracts (leaf 01, frozen):
//   - chat.submit ack: {turn_id, conversation_id, session_id, accepted, note};
//     accepted=false means the turn never started (note explains why).
//   - turn.terminal payload: 13 keys; the await loop filters events on
//     turn_id and consumes status/reply/error/duration_ms.
//
// Subscription ordering: bus.subscribe is established BEFORE chat.submit is
// sent, so the terminal event cannot slip through an event gap. Asserted by
// TestSubmitAndAwait_SubscribeBeforeSubmit.
//
// --await=off ack line (exact format, asserted by tests):
//
//	turn <id> accepted; run `meept chat --session <session-id>` to follow up

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/transport"
)

// DefaultLivenessTimeout is the no-progress ceiling for an awaited turn. A
// turn that shows no bus activity for this long reports a stall, but the
// error makes clear the task may still complete server-side. There is
// deliberately NO fixed wall-clock ceiling on the await itself: a turn that
// keeps making progress is waited on indefinitely (the old 110s sync wait
// and its "still running" stub are gone).
const DefaultLivenessTimeout = 120 * time.Second

// DefaultPollInterval is the CLI's bus.poll cadence.
const DefaultPollInterval = 250 * time.Millisecond

// chatSubmitTopics are the bus topics the CLI subscribes to before submit.
// turn.terminal carries the result; the wildcard agent/task/step topics carry
// progress activity that resets the liveness timer on long turns.
var chatSubmitTopics = []string{"turn.terminal", "agent.*", "task.*", "step.*"}

// cliSourceClient identifies CLI-originated chat.submit turns.
const cliSourceClient = "cli"

// chatTurnResult is the outcome of one submitted chat turn.
type chatTurnResult struct {
	Reply       string
	Status      string // completed|failed|timeout|parked
	Error       string
	AckSeconds  float64 // submit → ack latency
	TurnSeconds float64 // daemon-reported turn duration (duration_ms/1000)
}

// chatOpts controls submitAndAwait. Zero values are valid (defaults applied).
type chatOpts struct {
	// Await is "wait" (default) to await the terminal result, or "off" to
	// print the ack line and return immediately.
	Await string
	// Quiet suppresses the progress line even on a TTY.
	Quiet bool
	// Liveness is the no-progress timeout (default DefaultLivenessTimeout).
	Liveness time.Duration
	// PollInterval is the bus.poll cadence (default DefaultPollInterval).
	PollInterval time.Duration
	// Stdout/Stderr default to os.Stdout/os.Stderr (test seam).
	Stdout io.Writer
	Stderr io.Writer
	// isTTY overrides stdout TTY detection when non-nil (test seam).
	isTTY func() bool
}

// withDefaults fills unset fields.
func (o chatOpts) withDefaults() chatOpts {
	if o.Await == "" {
		o.Await = "wait"
	}
	if o.Liveness <= 0 {
		o.Liveness = DefaultLivenessTimeout
	}
	if o.PollInterval <= 0 {
		o.PollInterval = DefaultPollInterval
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.isTTY == nil {
		o.isTTY = stdoutIsTTY
	}
	return o
}

// stdoutIsTTY reports whether stdout is a terminal (stdlib-only check; the
// repo uses the same ModeCharDevice probe in cmd/meept/doctor.go).
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// chatSubmitAck mirrors the frozen chat.submit ack contract (leaf 01).
type chatSubmitAck struct {
	TurnID         string `json:"turn_id"`
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	Accepted       bool   `json:"accepted"`
	Note           string `json:"note"`
}

// turnTerminalPayload mirrors the frozen 13-key turn.terminal payload
// (internal/agent/handler.go TurnTerminalEvent). The await loop keys on
// TurnID and consumes Status/Reply/Error/DurationMS.
type turnTerminalPayload struct {
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	TaskID         string `json:"task_id"`
	IntentType     string `json:"intent_type"`
	AgentID        string `json:"agent_id"`
	HandlerCase    string `json:"handler_case"`
	Status         string `json:"status"`
	Reply          string `json:"reply"`
	DurationMS     int64  `json:"duration_ms"`
	ClassifiedBy   string `json:"classified_by"`
	Model          string `json:"model"`
	Error          string `json:"error"`
}

// busEvent is one event from a bus.poll response.
type busEvent struct {
	Topic     string
	Timestamp time.Time
	Payload   json.RawMessage
}

// submitAndAwait sends chat.submit and, unless opts.Await == "off", awaits
// the turn's turn.terminal event on a CLI bus subscription. While waiting it
// renders a single updating progress line — only when stdout is a TTY and
// --quiet was not passed; piped or quiet runs wait silently.
//
// Liveness: no bus activity for opts.Liveness (default 120s) fails the wait
// with "turn stalled (no progress for Ns); task may still complete — check
// `meept tasks`".
//
// With opts.Await == "off" it prints the ack line and returns without
// awaiting. The ack line format is fixed (see the file-header comment).
func submitAndAwait(ctx context.Context, client transport.Client, msg, sessionID string, opts chatOpts) (chatTurnResult, error) {
	opts = opts.withDefaults()
	start := time.Now()

	if opts.Await == "off" {
		ack, err := callChatSubmit(client, msg, sessionID)
		if err != nil {
			return chatTurnResult{}, err
		}
		if !ack.Accepted {
			return chatTurnResult{}, fmt.Errorf("turn rejected: %s", ack.Note)
		}
		fmt.Fprintf(opts.Stdout,
			"turn %s accepted; run `meept chat --session %s` to follow up\n",
			ack.TurnID, sessionID)
		return chatTurnResult{AckSeconds: time.Since(start).Seconds()}, nil
	}

	// Subscribe BEFORE submitting: the subscription must exist when the
	// daemon emits turn.terminal, or the event lands in an unsubscribed bus.
	subID, err := subscribeTurnTopics(client)
	if err != nil {
		return chatTurnResult{}, fmt.Errorf("bus subscribe failed: %w", err)
	}
	defer func() {
		_, _ = client.Call("bus.unsubscribe", map[string]string{"subscription_id": subID})
	}()
	// Only events recorded after the subscription exists are ours to read.
	since := time.Now()

	ack, err := callChatSubmit(client, msg, sessionID)
	if err != nil {
		return chatTurnResult{}, err
	}
	if !ack.Accepted {
		return chatTurnResult{}, fmt.Errorf("turn rejected: %s", ack.Note)
	}
	ackSeconds := time.Since(start).Seconds()

	progress := &turnProgressLine{w: opts.Stdout, turnID: ack.TurnID, start: start}
	progress.enabled = !opts.Quiet && opts.isTTY()

	lastProgress := time.Now()
	pollErrors := 0
	for {
		select {
		case <-ctx.Done():
			progress.finish()
			return chatTurnResult{AckSeconds: ackSeconds}, ctx.Err()
		case <-time.After(opts.PollInterval):
		}

		events, err := pollTurnEvents(client, subID, since)
		if err != nil {
			// A persistent poll failure (e.g. a subscription the daemon no
			// longer knows) must fail fast rather than masquerade as a stall.
			pollErrors++
			if pollErrors >= 5 {
				progress.finish()
				return chatTurnResult{AckSeconds: ackSeconds}, fmt.Errorf("bus poll failed: %w", err)
			}
			continue
		}
		pollErrors = 0

		if len(events) > 0 {
			lastProgress = time.Now()
			since = events[len(events)-1].Timestamp
		}
		for _, ev := range events {
			if ev.Topic != "turn.terminal" {
				continue // progress activity; resets liveness only
			}
			var payload turnTerminalPayload
			if err := json.Unmarshal(ev.Payload, &payload); err != nil {
				continue
			}
			if payload.TurnID != ack.TurnID {
				continue // someone else's turn
			}
			progress.finish()
			turnSeconds := float64(payload.DurationMS) / 1000.0
			if turnSeconds <= 0 {
				turnSeconds = time.Since(start).Seconds()
			}
			return chatTurnResult{
				Reply:       payload.Reply,
				Status:      payload.Status,
				Error:       payload.Error,
				AckSeconds:  ackSeconds,
				TurnSeconds: turnSeconds,
			}, nil
		}
		if time.Since(lastProgress) > opts.Liveness {
			progress.finish()
			return chatTurnResult{AckSeconds: ackSeconds}, fmt.Errorf(
				"turn stalled (no progress for %ds); task may still complete — check `meept tasks`",
				int(opts.Liveness.Seconds()))
		}
		progress.update(time.Now())
	}
}

// subscribeTurnTopics creates the CLI's bus subscription and returns the
// subscription id (mirrors internal/tui/events.go EventStream.subscribe).
func subscribeTurnTopics(client transport.Client) (string, error) {
	raw, err := client.Call("bus.subscribe", map[string]any{"topics": chatSubmitTopics})
	if err != nil {
		return "", err
	}
	var resp struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if resp.SubscriptionID == "" {
		return "", fmt.Errorf("daemon returned an empty subscription_id")
	}
	return resp.SubscriptionID, nil
}

// pollTurnEvents fetches events recorded after the given timestamp.
func pollTurnEvents(client transport.Client, subID string, since time.Time) ([]busEvent, error) {
	raw, err := client.Call("bus.poll", map[string]string{
		"subscription_id": subID,
		"since":           since.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Events []struct {
			Topic     string          `json:"topic"`
			Timestamp time.Time       `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	events := make([]busEvent, 0, len(resp.Events))
	for _, e := range resp.Events {
		events = append(events, busEvent{Topic: e.Topic, Timestamp: e.Timestamp, Payload: e.Payload})
	}
	return events, nil
}

// callChatSubmit sends the chat.submit RPC (never the legacy blocking "chat").
// conversation_id is omitted: the daemon mints one and reports it in the ack.
func callChatSubmit(client transport.Client, msg, sessionID string) (chatSubmitAck, error) {
	params := map[string]any{
		"message":       msg,
		"session_id":    sessionID,
		"source_client": cliSourceClient,
	}
	raw, err := client.Call("chat.submit", params)
	if err != nil {
		return chatSubmitAck{}, fmt.Errorf("chat submit failed: %w", err)
	}
	var ack chatSubmitAck
	if err := json.Unmarshal(raw, &ack); err != nil {
		return chatSubmitAck{}, fmt.Errorf("chat submit ack unparseable: %w", err)
	}
	return ack, nil
}

// submitChatTurn runs one async turn for the CLI's non-TUI chat paths and
// renders the outcome with the existing CLI conventions: the reply goes to
// stdout; a failed turn returns an error carrying the daemon's error text
// (main prints it on stderr and exits 1 — same path as every RunE error).
func submitChatTurn(client transport.Client, message, sessionID string, opts chatOpts) error {
	res, err := submitAndAwait(context.Background(), client, message, sessionID, opts)
	if err != nil {
		return err
	}
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}
	if res.Reply != "" {
		out := opts.Stdout
		if out == nil {
			out = os.Stdout
		}
		fmt.Fprintln(out, res.Reply)
	}
	return nil
}

// turnProgressLine renders the single updating progress line
// ("\rturn <id> · 12s") when enabled. Disabled for piped stdout and --quiet.
type turnProgressLine struct {
	w       io.Writer
	enabled bool
	turnID  string
	start   time.Time
	last    string
}

// update redraws the line at one-second granularity.
func (p *turnProgressLine) update(now time.Time) {
	if !p.enabled {
		return
	}
	line := fmt.Sprintf("turn %s · %ds", p.turnID, int(now.Sub(p.start).Seconds()))
	if line == p.last {
		return
	}
	if pad := len(p.last) - len(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	fmt.Fprintf(p.w, "\r%s", line)
	p.last = line
}

// finish erases the line so the reply prints on a clean terminal row.
func (p *turnProgressLine) finish() {
	if !p.enabled {
		return
	}
	fmt.Fprintf(p.w, "\r%s\r", strings.Repeat(" ", len(p.last)))
	p.last = ""
}
