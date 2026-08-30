package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// --- Task 1: Source field on RewakePayload -------------------------------

// TestRewake_SourceIsMetadataNotFilter pins the Source field's contract:
// it travels the wire, is provenance-only, and unknown/empty/garbage
// sources all inject identically. Source must never gate delivery.
func TestRewake_SourceIsMetadataNotFilter(t *testing.T) {
	tests := []struct {
		name           string
		raw            string // JSON as a publisher would emit it
		wantSource     string // parsed field value
		wantVia        string // substring the injected note must carry ("" = must NOT)
		wantNotePrefix bool   // full rewakeNotePrefix present
	}{
		{
			name:           "source timer parses and injects",
			raw:            `{"session_id":"conv-1","hook_type":"scheduler_job","hook_name":"scheduler:nightly","source":"timer"}`,
			wantSource:     "timer",
			wantVia:        "via timer",
			wantNotePrefix: true,
		},
		{
			name:           "source hook parses and injects",
			raw:            `{"session_id":"conv-1","source":"hook"}`,
			wantSource:     "hook",
			wantVia:        "via hook",
			wantNotePrefix: true,
		},
		{
			name:           "garbage source still injects identically",
			raw:            `{"session_id":"conv-1","source":"bogus-source-🛸"}`,
			wantSource:     "bogus-source-🛸",
			wantVia:        "via bogus-source-🛸",
			wantNotePrefix: true,
		},
		{
			name:           "empty source still injects, renders no via",
			raw:            `{"session_id":"conv-1"}`,
			wantSource:     "",
			wantVia:        "",
			wantNotePrefix: true,
		},
		{
			name:           "bare payload without source field",
			raw:            `{"session_id":"conv-1","hook_type":"session_start","hook_name":"http:x"}`,
			wantSource:     "",
			wantNotePrefix: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := newRewakeTestLoop(t)
			conv := NewConversationStore(10).Get("conv-1")

			publishRewake(t, loop.bus, json.RawMessage(tt.raw))

			got := waitRewake(t, loop.rewakeCh, 2*time.Second)
			if got.Source != tt.wantSource {
				t.Errorf("parsed source = %q, want %q", got.Source, tt.wantSource)
			}
			// waitRewake consumed the signal above; requeue the parsed
			// payload so injectRewakes drains it deterministically (the
			// production path does exactly this read, just concurrently).
			loop.rewakeCh <- got

			loop.injectRewakes(conv, "conv-1", 1)

			note := conv.LastUserMessage()
			if note == "" {
				t.Fatal("expected injected note, got none — Source filtered delivery")
			}
			if tt.wantVia != "" {
				if !strings.Contains(note, tt.wantVia) {
					t.Errorf("note %q missing %q", note, tt.wantVia)
				}
			} else if strings.Contains(note, "via ") {
				t.Errorf("empty-source note %q unexpectedly carries a via clause", note)
			}
			if tt.wantNotePrefix && !strings.Contains(note, rewakeNotePrefix) {
				t.Errorf("note %q missing prefix", note)
			}
		})
	}
}

// TestRewake_OldPublishersUnaffected verifies the additive field does not
// disturb the legacy JSON round-trip (http_hooks/file_watcher payloads
// carry no source key).
func TestRewake_OldPublishersUnaffected(t *testing.T) {
	legacy := RewakePayload{SessionID: "c1", HookType: "t", HookName: "n"}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"source"`) {
		t.Errorf("empty Source must be omitted from JSON, got %s", b)
	}
	var back RewakePayload
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != legacy {
		t.Errorf("round-trip changed payload: %+v != %+v", back, legacy)
	}
}

// --- Task 3: primary loop is armed ---------------------------------------

// TestRewake_PrimaryLoopArmedOnRunOnce proves the PRIMARY daemon loop
// arms the rewake consumer as a side effect of a normal user turn —
// not via an agent-registry-only path. RunOnceWithParts is the single
// turn entry every daemon chat path funnels through (ChatHandler,
// worker_pool, cluster_resources), so one grep-visible arm call there
// covers the primary loop. The proof is behavioral: after RunOnce with a
// live bus, drainRewakes returns buffered signals, which is only
// possible if the consumer goroutine is running.
func TestRewake_PrimaryLoopArmedOnRunOnce(t *testing.T) {
	mb := bus.New(nil, rewindTestLogger())
	defer mb.Close()

	loop := NewAgentLoop(
		"test-session",
		"/tmp",
		WithMessageBus(mb),
		WithLoopLogger(rewindTestLogger()),
		WithLLMChatter(newMockChatter(llmResponse("hello from primary loop"))),
	)
	t.Cleanup(loop.stopRewake)

	// Explicitly NOT calling armRewakeConsumer: RunOnceWithParts must do it.
	if _, err := loop.RunOnce(t.Context(), "hi", "conv-arm"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// The armed consumer pumps published signals into rewakeCh; drainRewakes
	// reads rewakeCh. If the loop never armed, this stays empty forever.
	publishRewake(t, mb, map[string]any{
		"session_id": "conv-arm",
		"hook_type":  "session_start",
		"hook_name":  "http:proof",
		"source":     "hook",
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := loop.drainRewakes("conv-arm"); len(got) > 0 {
			if got[0].Source != "hook" {
				t.Errorf("source = %q, want hook", got[0].Source)
			}
			if got[0].HookName != "http:proof" {
				t.Errorf("hook_name = %q, want http:proof", got[0].HookName)
			}
			return // armed: consumer received the published signal
		}
		if time.Now().After(deadline) {
			t.Fatal("primary loop did not arm rewake consumer on RunOnce — drainRewakes stayed empty")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// llmResponse builds a minimal successful llm.Response for mock chatters.
func llmResponse(content string) *llm.Response {
	return &llm.Response{Content: content, FinishReason: "stop"}
}
