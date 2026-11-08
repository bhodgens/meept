package contact

// Gate tests for contract C2 (master.md, quoted verbatim): the
// ChannelAttempt seam + ChainRunner. Written BEFORE implementation
// (00T-gate-tests.md, T2). RED until leaf 01/02 land chain.go.
//
// Pinned signatures:
//
//	type ChannelAttempt interface {
//	    Channel() Channel
//	    Attempt(ctx context.Context, rec Record, message string) (outcome string, err error)
//	}
//	func NewChainRunner(attempts map[Channel]ChannelAttempt, delays map[Channel]time.Duration) *ChainRunner
//	func (c *ChainRunner) Run(ctx, rec, message, override []Channel) ChainReport
//
// ChainReport: per-attempt {channel, outcome, err, duration_ms}, ONE chain
// execution id shared by every attempt of the run. Named outcomes:
// "answered" | "delivered" | "no_answer" | "unavailable" | "busy" | "failed".
// err == nil means CONNECTED/DELIVERED. Synchronous for message channels;
// voice-shaped Attempt returns at terminal/no-answer (a connected call is
// NOT a chain failure and does NOT block the tool result).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeAttempt records invocations and plays a scripted outcome.
type fakeAttempt struct {
	mu      sync.Mutex
	channel Channel
	outcome string // returned as (outcome, nil) when ok, else (outcome, err)
	err     error
	calls   []fakeAttemptCall
}

type fakeAttemptCall struct {
	rec     Record
	message string
}

func (f *fakeAttempt) Channel() Channel { return f.channel }

func (f *fakeAttempt) Attempt(_ context.Context, rec Record, message string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeAttemptCall{rec: rec, message: message})
	if f.err != nil {
		return f.outcome, f.err
	}
	return f.outcome, nil
}

func (f *fakeAttempt) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// recWithPolicy builds a record whose policy IS the given chain.
func recWithPolicy(name string, policy ...Channel) Record {
	rec := mustRecord(name, "+15550000010")
	rec.Policy = policy
	return rec
}

// TestChainStopsOnDelivered is the leaf's canonical worked expansion:
// attempts voice:no_answer -> signal_voice:busy -> signal_msg:delivered
// run in policy order, exactly 3 calls happen, report marks channel 3
// delivered, and every attempt shares ONE chain id.
func TestChainStopsOnDelivered(t *testing.T) {
	voice := &fakeAttempt{channel: ChannelTelnyxVoice, outcome: "no_answer",
		err: errors.New("no_answer")}
	sigVoice := &fakeAttempt{channel: ChannelSignalVoice, outcome: "busy",
		err: errors.New("busy")}
	sigMsg := &fakeAttempt{channel: ChannelSignalMsg, outcome: "delivered"}

	runner := NewChainRunner(
		map[Channel]ChannelAttempt{
			ChannelTelnyxVoice: voice,
			ChannelSignalVoice: sigVoice,
			ChannelSignalMsg:   sigMsg,
		},
		nil, // no inter-channel delays in the canonical test
	)

	rec := recWithPolicy("ada",
		ChannelTelnyxVoice, ChannelSignalVoice, ChannelSignalMsg)
	report := runner.Run(context.Background(), rec, "lunch at noon?", nil)

	attempts := reportAttempts(report)
	if len(attempts) != 3 {
		t.Fatalf("report attempts = %d, want 3", len(attempts))
	}
	if voice.count() != 1 || sigVoice.count() != 1 || sigMsg.count() != 1 {
		t.Fatalf("attempt call counts = (voice %d, sigVoice %d, sigMsg %d), want (1,1,1)",
			voice.count(), sigVoice.count(), sigMsg.count())
	}
	if attempts[0].Channel != ChannelTelnyxVoice || attempts[1].Channel != ChannelSignalVoice ||
		attempts[2].Channel != ChannelSignalMsg {
		t.Errorf("attempt order = %v, want policy order (telnyx_voice, signal_voice, signal_msg)",
			[]Channel{attempts[0].Channel, attempts[1].Channel, attempts[2].Channel})
	}
	if attempts[2].Outcome != "delivered" {
		t.Errorf("attempts[2].Outcome = %q, want %q", attempts[2].Outcome, "delivered")
	}
	if chainID(report) == "" {
		t.Errorf("report has an empty chain execution id (C2: one chain id per run)")
	}
	for i, a := range attempts {
		if a.ChainID != chainID(report) {
			t.Errorf("attempt %d ChainID = %q, want the run's chain id %q", i, a.ChainID, chainID(report))
		}
	}
}

// TestChainFirstDeliveredStops pins stop-on-delivered: an answered voice
// call (err == nil) is terminal; NO later channel is attempted.
func TestChainFirstDeliveredStops(t *testing.T) {
	voice := &fakeAttempt{channel: ChannelTelnyxVoice, outcome: "answered"}
	sigMsg := &fakeAttempt{channel: ChannelSignalMsg, outcome: "delivered"}

	runner := NewChainRunner(map[Channel]ChannelAttempt{
		ChannelTelnyxVoice: voice,
		ChannelSignalMsg:   sigMsg,
	}, nil)

	rec := recWithPolicy("bob", ChannelTelnyxVoice, ChannelSignalMsg)
	report := runner.Run(context.Background(), rec, "hi", nil)

	if sigMsg.count() != 0 {
		t.Errorf("signal_msg attempted %d times after voice answered, want 0 (stop-on-delivered)", sigMsg.count())
	}
	attempts := reportAttempts(report)
	if len(attempts) != 1 || attempts[0].Outcome != "answered" {
		t.Fatalf("report = %+v, want one answered attempt", attempts)
	}
}

// TestChainNamedOutcomes pins the C2 outcome vocabulary across the
// failure fallthrough table: busy, unavailable, failed, no_answer all
// fall through; delivered and answered stop.
func TestChainNamedOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		stop    bool // whether this outcome must END the chain
	}{
		{"answered", "answered", true},
		{"delivered", "delivered", true},
		{"no_answer", "no_answer", false},
		{"unavailable", "unavailable", false},
		{"busy", "busy", false},
		{"failed", "failed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := &fakeAttempt{channel: ChannelSignalMsg, outcome: tc.outcome,
				err: errFor(tc.outcome, tc.stop)}
			second := &fakeAttempt{channel: ChannelTelnyxSMS, outcome: "delivered"}

			runner := NewChainRunner(map[Channel]ChannelAttempt{
				ChannelSignalMsg: first,
				ChannelTelnyxSMS: second,
			}, nil)

			rec := recWithPolicy("cat", ChannelSignalMsg, ChannelTelnyxSMS)
			report := runner.Run(context.Background(), rec, "m", nil)
			attempts := reportAttempts(report)

			if tc.stop && second.count() != 0 {
				t.Errorf("outcome %q stopped the chain late: second channel attempted %d times",
					tc.outcome, second.count())
			}
			if !tc.stop {
				if len(attempts) != 2 {
					t.Fatalf("outcome %q attempts = %d, want 2 (fallthrough)", tc.outcome, len(attempts))
				}
				if attempts[1].Outcome != "delivered" {
					t.Errorf("fallback attempt outcome = %q, want delivered", attempts[1].Outcome)
				}
			}
		})
	}
}

// errFor: delivered/answered succeed (err nil); everything else fails
// with an outcome-shaped error.
func errFor(outcome string, stop bool) error {
	if stop {
		return nil
	}
	return errors.New("contact: " + outcome)
}

// TestChainOverridePinsOrder pins the Run override parameter: an override
// list replaces the record policy for THIS run only (C6 arg `channels`).
func TestChainOverridePinsOrder(t *testing.T) {
	sigMsg := &fakeAttempt{channel: ChannelSignalMsg, outcome: "delivered"}
	sms := &fakeAttempt{channel: ChannelTelnyxSMS, outcome: "delivered"}

	runner := NewChainRunner(map[Channel]ChannelAttempt{
		ChannelSignalMsg: sigMsg,
		ChannelTelnyxSMS: sms,
	}, nil)

	// Record policy says signal_msg first; the run override demands SMS.
	rec := recWithPolicy("dee", ChannelSignalMsg, ChannelTelnyxSMS)
	report := runner.Run(context.Background(), rec, "m",
		[]Channel{ChannelTelnyxSMS})

	attempts := reportAttempts(report)
	if len(attempts) == 0 || attempts[0].Channel != ChannelTelnyxSMS {
		t.Errorf("override ignored: first attempt = %+v, want telnyx_sms first", attempts)
	}
	if sigMsg.count() != 0 {
		t.Errorf("signal_msg attempted %d times under override, want 0", sigMsg.count())
	}
}

// TestChainDelayRespected pins the delays map with SHORT test clocks:
// the runner waits the configured delay before the SECOND channel.
func TestChainDelayRespected(t *testing.T) {
	first := &fakeAttempt{channel: ChannelSignalMsg, outcome: "busy",
		err: errors.New("contact: busy")}
	second := &fakeAttempt{channel: ChannelTelnyxSMS, outcome: "delivered"}

	const delay = 40 * time.Millisecond
	runner := NewChainRunner(map[Channel]ChannelAttempt{
		ChannelSignalMsg: first,
		ChannelTelnyxSMS: second,
	}, map[Channel]time.Duration{ChannelTelnyxSMS: delay})

	rec := recWithPolicy("eve", ChannelSignalMsg, ChannelTelnyxSMS)
	report := runner.Run(context.Background(), rec, "m", nil)

	attempts := reportAttempts(report)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	if attempts[1].DurationMS < 40 {
		t.Errorf("fallback attempt duration_ms = %d, want >= %d (delay respected)",
			attempts[1].DurationMS, delay.Milliseconds())
	}
}

// TestChainAttemptSeesRecordAndMessage pins the seam inputs: every
// attempted channel receives the SAME record and message.
func TestChainAttemptSeesRecordAndMessage(t *testing.T) {
	sigVoice := &fakeAttempt{channel: ChannelSignalVoice, outcome: "no_answer",
		err: errors.New("contact: no_answer")}
	sigMsg := &fakeAttempt{channel: ChannelSignalMsg, outcome: "delivered"}

	runner := NewChainRunner(map[Channel]ChannelAttempt{
		ChannelSignalVoice: sigVoice,
		ChannelSignalMsg:   sigMsg,
	}, nil)

	rec := recWithPolicy("fin", ChannelSignalVoice, ChannelSignalMsg)
	const msg = "standup in 5"
	runner.Run(context.Background(), rec, msg, nil)

	for name, f := range map[string]*fakeAttempt{"signal_voice": sigVoice, "signal_msg": sigMsg} {
		if f.count() != 1 {
			t.Fatalf("%s attempted %d times, want 1", name, f.count())
		}
		if got := f.calls[0].message; got != msg {
			t.Errorf("%s saw message %q, want %q", name, got, msg)
		}
		if got := f.calls[0].rec.ID; got != rec.ID {
			t.Errorf("%s saw record id %q, want %q", name, got, rec.ID)
		}
	}
}

// TestChainUnknownChannelSkipped pins fail-soft: a policy channel with NO
// registered attempt is skipped, not a run-abort.
func TestChainUnknownChannelSkipped(t *testing.T) {
	sigMsg := &fakeAttempt{channel: ChannelSignalMsg, outcome: "delivered"}
	runner := NewChainRunner(map[Channel]ChannelAttempt{
		ChannelSignalMsg: sigMsg,
	}, nil)

	// telnyx_voice has no registered attempt in this runner.
	rec := recWithPolicy("gus", ChannelTelnyxVoice, ChannelSignalMsg)
	report := runner.Run(context.Background(), rec, "m", nil)

	if sigMsg.count() != 1 {
		t.Fatalf("signal_msg attempted %d times, want 1 (missing channel skipped, not aborted)", sigMsg.count())
	}
	attempts := reportAttempts(report)
	if len(attempts) != 1 || attempts[0].Channel != ChannelSignalMsg {
		t.Errorf("report = %+v, want only the registered attempt", attempts)
	}
}
