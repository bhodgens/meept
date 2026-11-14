package daemon

// Defect 2026-11-14 pins for the voice reasoning slot:
//   - boundReasoning truncates RUNE-SAFELY: a raw byte cut split multi-byte
//     UTF-8 runes and handed the model a corrupted tail.
//   - the retainedReason map is BOUNDED at maxRetainedReasoningSessions:
//     it previously grew one entry per session id for the daemon's whole
//     life; eviction drops the oldest-turned session (last-N by update
//     order), and a recently-used session survives a newer session's turn.

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/voice"
)

// TestBoundReasoningRuneSafe: truncation never splits a UTF-8 rune — the
// retained text stays valid UTF-8 and keeps every whole rune inside the
// cap (the marker is appended after the last COMPLETE rune).
func TestBoundReasoningRuneSafe(t *testing.T) {
	// ASCII prefix, then a run of 3-byte runes straddling the cap.
	ascii := strings.Repeat("a", 100)
	multibyte := strings.Repeat("漢", 2000) // 6000 bytes of 3-byte runes
	r := ascii + multibyte

	got := boundReasoning(r)
	if !utf8.ValidString(got) {
		t.Fatalf("boundReasoning produced invalid UTF-8: %.80q", got)
	}
	if !strings.HasSuffix(got, "…retained reasoning truncated]") {
		t.Fatalf("truncated block must carry the marker: %.80q", got)
	}
	if len(got) > maxPriorReasoningBytes+len("\n[…retained reasoning truncated]") {
		t.Errorf("bounded length %d exceeds the cap plus the marker", len(got))
	}
	// The whole ASCII prefix survived (it sits inside the cap by
	// construction).
	if !strings.HasPrefix(got, ascii) {
		t.Error("the in-cap ASCII prefix must survive truncation")
	}

	// Pure multibyte over the cap: still valid, still marked.
	got2 := boundReasoning(multibyte)
	if !utf8.ValidString(got2) {
		t.Fatalf("pure-multibyte truncation produced invalid UTF-8")
	}
	if !strings.Contains(got2, "漢") {
		t.Error("expected whole runes to survive the truncation")
	}

	// At/under the cap returns the input untouched (including
	// multibyte).
	if got := boundReasoning("短い"); got != "短い" {
		t.Errorf("under-cap text must pass through untouched, got %q", got)
	}
}

// TestVoiceReasoningRetentionMapBounded: minting more distinct sessions
// than maxRetainedReasoningSessions must never grow the map past the cap —
// the oldest-turned slots are evicted (pre-fix the map grew unbounded per
// session id for the daemon's whole life).
func TestVoiceReasoningRetentionMapBounded(t *testing.T) {
	r, chatter, sessions := reasonRunner(t, "ok", "")
	total := maxRetainedReasoningSessions + 4
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		chatter.set("ok", "reasoning for session slot")
		sess, err := sessions.Create("bounded slot session")
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, sess.ID)
		handle := voice.SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}
		if _, err := r.Run(context.Background(), handle, "turn"); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		r.reasonMu.Lock()
		n := len(r.retainedReason)
		r.reasonMu.Unlock()
		if n > maxRetainedReasoningSessions {
			t.Fatalf("retainedReason grew to %d entries (cap %d) — the map must stay bounded", n, maxRetainedReasoningSessions)
		}
	}
	r.reasonMu.Lock()
	n := len(r.retainedReason)
	_, oldestStill := r.retainedReason[ids[0]]
	_, newestStill := r.retainedReason[ids[total-1]]
	r.reasonMu.Unlock()
	if n != maxRetainedReasoningSessions {
		t.Fatalf("final map size %d, want exactly the cap %d", n, maxRetainedReasoningSessions)
	}
	// The OLDEST slot was evicted (each session turned exactly once, so
	// ids[0] is the oldest-turned); the NEWEST survived.
	if oldestStill {
		t.Error("the oldest-turned session must be evicted at cap")
	}
	if !newestStill {
		t.Error("the newest-turned session must survive at cap")
	}
}

// TestVoiceReasoningRetentionRecentUseSurvives: last-N is by UPDATE
// order — a session that turned again refreshes its slot, so a live
// conversation is not evicted by a burst of one-turn sessions.
func TestVoiceReasoningRetentionRecentUseSurvives(t *testing.T) {
	r, chatter, sessions := reasonRunner(t, "ok", "")
	// Session LIVE turns first, then refreshes AFTER the burst.
	sessLive, err := sessions.Create("live voice session")
	if err != nil {
		t.Fatalf("Create live: %v", err)
	}
	handleLive := voice.SessionHandle{ID: sessLive.ID, ConversationID: sessLive.ConversationID}
	chatter.set("ok", "live reasoning v1")
	if _, err := r.Run(context.Background(), handleLive, "first"); err != nil {
		t.Fatalf("Run live 1: %v", err)
	}
	// Burst: fill the map to the cap with one-turn sessions.
	for i := 0; i < maxRetainedReasoningSessions; i++ {
		chatter.set("ok", "burst reasoning")
		sess, err := sessions.Create("burst session")
		if err != nil {
			t.Fatalf("Create burst %d: %v", i, err)
		}
		handle := voice.SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}
		if _, err := r.Run(context.Background(), handle, "turn"); err != nil {
			t.Fatalf("Run burst %d: %v", i, err)
		}
	}
	// LIVE turns again: its slot is refreshed to most-recent.
	chatter.set("ok", "live reasoning v2")
	if _, err := r.Run(context.Background(), handleLive, "again"); err != nil {
		t.Fatalf("Run live 2: %v", err)
	}
	// One more fresh session fits WITHOUT evicting the refreshed slot.
	chatter.set("ok", "final fresh reasoning")
	sessLast, err := sessions.Create("post-refresh session")
	if err != nil {
		t.Fatalf("Create last: %v", err)
	}
	handleLast := voice.SessionHandle{ID: sessLast.ID, ConversationID: sessLast.ConversationID}
	if _, err := r.Run(context.Background(), handleLast, "turn"); err != nil {
		t.Fatalf("Run last: %v", err)
	}

	r.reasonMu.Lock()
	retained := r.retainedReason[sessLive.ID]
	r.reasonMu.Unlock()
	if retained == "" {
		t.Fatal("the refreshed (recently-used) session was evicted — eviction must be oldest-TURNED first")
	}
	if !strings.Contains(retained, "live reasoning v2") {
		t.Errorf("refreshed slot holds %q, want the latest turn's reasoning", retained)
	}
}
