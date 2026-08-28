package agent

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// rewindTestLogger returns a quiet logger for rewake tests.
func rewindTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitRewake polls rewakeCh until a payload arrives or the timeout
// elapses. The consumer is asynchronous (bus → pump goroutine → channel),
// so tests poll rather than sleep a fixed interval.
func waitRewake(t *testing.T, ch chan RewakePayload, timeout time.Duration) RewakePayload {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case p := <-ch:
			return p
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for rewake signal on loop channel")
	return RewakePayload{}
}

// newRewakeTestLoop builds a loop wired to a live bus and arms its
// rewake consumer, mirroring what RunOnce does at turn entry.
func newRewakeTestLoop(t *testing.T, opts ...LoopOption) *AgentLoop {
	t.Helper()
	mb := bus.New(nil, rewindTestLogger())
	all := append([]LoopOption{WithMessageBus(mb), WithLoopLogger(rewindTestLogger())}, opts...)
	loop := NewAgentLoop("test-session", "/tmp", all...)
	loop.armRewakeConsumer()
	t.Cleanup(func() {
		loop.stopRewake()
		mb.Close()
	})
	return loop
}

// publishRewake publishes a hook.async_rewake signal the same way the
// HTTPHook and FileWatcherHook publishers do.
func publishRewake(t *testing.T, mb *bus.MessageBus, payload any) {
	t.Helper()
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "hook", payload)
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	mb.Publish(HookAsyncRewakeTopic, msg)
}

// TestRewake_WakeFires_FromPublishedSignal is the end-to-end contract
// proof: a published hook.async_rewake signal reaches the loop's rewake
// channel — which injectRewakes turns into a conversation injection (the
// wake) at the top of the next reasoning iteration.
func TestRewake_WakeFires_FromPublishedSignal(t *testing.T) {
	loop := newRewakeTestLoop(t)

	publishRewake(t, loop.bus, map[string]any{
		"session_id": "conv-1",
		"hook_type":  "session_start",
		"hook_name":  "http:http://example.test/hook",
	})

	got := waitRewake(t, loop.rewakeCh, 2*time.Second)
	if got.SessionID != "conv-1" {
		t.Errorf("session_id = %q, want conv-1", got.SessionID)
	}
	if got.HookType != "session_start" {
		t.Errorf("hook_type = %q, want session_start", got.HookType)
	}
	if got.HookName != "http:http://example.test/hook" {
		t.Errorf("hook_name = %q, unexpected", got.HookName)
	}
}

// TestRewake_InjectRewakes_Table covers the conversation-injection
// matrix: which signals are injected, which are dropped, and what the
// injected note carries.
func TestRewake_InjectRewakes_Table(t *testing.T) {
	tests := []struct {
		name         string
		payload      RewakePayload
		conversation string
		wantInConv   bool
		wantContent  []string // substrings required in the injected note
	}{
		{
			name:         "matching session injected",
			payload:      RewakePayload{SessionID: "conv-1", HookType: "session_start", HookName: "http:x"},
			conversation: "conv-1",
			wantInConv:   true,
			wantContent:  []string{rewakeNotePrefix, "http:x"},
		},
		{
			name:         "broadcast (empty session) injected",
			payload:      RewakePayload{SessionID: "", HookType: "file_watcher", HookName: "file:*.txt", Path: "/tmp/watched.txt"},
			conversation: "conv-2",
			wantInConv:   true,
			wantContent:  []string{rewakeNotePrefix, "file:*.txt", "/tmp/watched.txt"},
		},
		{
			name:         "other session dropped",
			payload:      RewakePayload{SessionID: "conv-other", HookType: "session_end", HookName: "http:x"},
			conversation: "conv-1",
			wantInConv:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := newRewakeTestLoop(t)
			conv := NewConversationStore(10).Get("conv-test")

			loop.rewakeCh <- tt.payload
			loop.injectRewakes(conv, tt.conversation, 1)

			if tt.wantInConv {
				note := conv.LastUserMessage()
				if note == "" {
					t.Fatal("expected injected note, got none")
				}
				for _, want := range tt.wantContent {
					if !strings.Contains(note, want) {
						t.Errorf("note %q missing %q", note, want)
					}
				}
			} else if conv.LastUserMessage() != "" {
				t.Errorf("expected no injection, got %q", conv.LastUserMessage())
			}
		})
	}
}

// TestRewake_ArmIdempotent proves arming twice does not double-subscribe:
// one published signal yields exactly one payload.
func TestRewake_ArmIdempotent(t *testing.T) {
	loop := newRewakeTestLoop(t)
	loop.armRewakeConsumer() // second arm must be a no-op
	loop.armRewakeConsumer()

	publishRewake(t, loop.bus, map[string]any{"session_id": "conv-1", "hook_type": "t"})

	got := waitRewake(t, loop.rewakeCh, 2*time.Second)
	if got.SessionID != "conv-1" {
		t.Errorf("session_id = %q, want conv-1", got.SessionID)
	}
	select {
	case p := <-loop.rewakeCh:
		t.Errorf("unexpected extra payload %+v (double subscription?)", p)
	default:
	}
}

// TestRewake_MalformedPayload_Skipped proves a malformed payload does not
// kill the consumer: the bad signal is skipped and the next valid one
// still arrives.
func TestRewake_MalformedPayload_Skipped(t *testing.T) {
	loop := newRewakeTestLoop(t)

	loop.bus.Publish(HookAsyncRewakeTopic, &models.BusMessage{
		Type:    models.MessageTypeEvent,
		Payload: json.RawMessage(`{"session_id": `),
	})
	publishRewake(t, loop.bus, map[string]any{"session_id": "conv-1", "hook_type": "t"})

	got := waitRewake(t, loop.rewakeCh, 2*time.Second)
	if got.SessionID != "conv-1" {
		t.Errorf("session_id = %q, want conv-1 (consumer died after malformed payload?)", got.SessionID)
	}
}

// TestRewake_StopRewake_SafeAndIdempotent proves teardown is safe when
// never armed and idempotent when armed.
func TestRewake_StopRewake_SafeAndIdempotent(t *testing.T) {
	// Never armed: must not panic or block.
	plain := NewAgentLoop("s", "/tmp")
	plain.stopRewake()

	// Armed then stopped twice: must not panic or block. rewakeCh is
	// deliberately NOT nil'd on stop (close-then-nil races the loop
	// goroutine's reads in drainRewakes); idempotency comes from
	// rewakeStopOnce — the double stopRewake must be a no-op.
	loop := newRewakeTestLoop(t)
	loop.stopRewake()
	loop.stopRewake()
}

// TestRewake_CloseStopsConsumer proves Close tears down the consumer
// without deadlocking. rewakeCh stays non-nil after Close (never nil'd —
// close-then-nil would race drainRewakes' reads); drainRewakes still
// returns nil because the closed channel drains empty.
func TestRewake_CloseStopsConsumer(t *testing.T) {
	loop := newRewakeTestLoop(t)
	done := make(chan struct{})
	go func() {
		loop.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close deadlocked")
	}
	if got := loop.drainRewakes("conv-1"); got != nil {
		t.Errorf("drainRewakes after Close = %v, want nil", got)
	}
}

// TestRewake_AfterStopDrainIsNoop proves drainRewakes is a no-op on a
// stopped loop (nil channel).
func TestRewake_AfterStopDrainIsNoop(t *testing.T) {
	loop := newRewakeTestLoop(t)
	loop.stopRewake()
	if got := loop.drainRewakes("conv-1"); got != nil {
		t.Errorf("drainRewakes after stop = %v, want nil", got)
	}
}

// TestParseRewakePayload covers payload decoding, including %w wrapping.
func TestParseRewakePayload(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    RewakePayload
		wantErr bool
	}{
		{
			name: "full payload",
			raw:  `{"session_id":"s1","hook_type":"file_watcher","hook_name":"file:*.log","path":"/a/b.log"}`,
			want: RewakePayload{SessionID: "s1", HookType: "file_watcher", HookName: "file:*.log", Path: "/a/b.log"},
		},
		{
			name: "minimal payload",
			raw:  `{"session_id":"s2","hook_type":"session_end","hook_name":"http:x"}`,
			want: RewakePayload{SessionID: "s2", HookType: "session_end", HookName: "http:x"},
		},
		{
			name:    "invalid json",
			raw:     `{`,
			wantErr: true,
		},
		{
			name:    "wrong type for field",
			raw:     `{"session_id":42}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRewakePayload(json.RawMessage(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got payload %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRewakePayload: %v", err)
			}
			if got != tt.want {
				t.Errorf("payload = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestRewake_NoteWithoutHookName uses the bare prefix when neither path
// nor hook name is present.
func TestRewake_NoteWithoutHookName(t *testing.T) {
	if got := rewakeNote(RewakePayload{SessionID: "s"}); got != rewakeNotePrefix {
		t.Errorf("rewakeNote bare = %q, want prefix %q", got, rewakeNotePrefix)
	}
}
