package agent

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

// newPrivacyTestDispatcher builds a dispatcher wired to a temp-dir metrics
// store, with an injected hasher over a fixed salt (mirrors the daemon
// wiring in internal/daemon/daemon.go).
func newPrivacyTestDispatcher(t *testing.T) (*Dispatcher, *metrics.Store) {
	t.Helper()
	s, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	d := NewDispatcher(DispatcherConfig{})
	d.SetMetricsStore(s)
	d.SetInputHasher(func(message string) string {
		return metrics.HashInput("deadbeefdeadbeef", []byte("0123456789abcdef0123456789abcdef"), message)
	})
	return d, s
}

// TestRecordDispatch_HashesModelTurnNoSummaryEmpty covers the S4 headline
// behavior: 16-hex input_hash, empty input_summary, Intent.Model
// provenance, and turn_no incrementing across two same-session dispatches.
func TestRecordDispatch_HashesModelTurnNoSummaryEmpty(t *testing.T) {
	d, store := newPrivacyTestDispatcher(t)

	d.RecordDispatch("sess-1", "route_to_agent", "fix the login bug on /checkout",
		&DispatchResult{
			AgentID: "debugger",
			Intent:  &Intent{Type: "debug", Confidence: 0.9, Method: "llm", Model: "provider/model-1"},
		}, false, nil)
	d.RecordDispatch("sess-1", "default_route", "second message",
		&DispatchResult{
			AgentID: "generalist",
			Intent:  &Intent{Type: "general", Confidence: 0.8, Method: "keyword"}, // deterministic door: Model ""
		}, false, nil)

	entries, err := store.QueryDispatchLogBySession("sess-1", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d rows, want 2", len(entries))
	}
	// ORDER BY id DESC: second dispatch first.
	second, first := entries[0], entries[1]

	// Privacy headline: input_summary NEVER persisted.
	if first.InputSummary != "" || second.InputSummary != "" {
		t.Errorf("input_summary persisted: %q / %q", first.InputSummary, second.InputSummary)
	}

	// 16-hex salted hash of the raw input; differs between messages.
	wantFirst := metrics.HashInput("deadbeefdeadbeef", []byte("0123456789abcdef0123456789abcdef"), "fix the login bug on /checkout")
	if first.InputHash != wantFirst || len(first.InputHash) != 16 {
		t.Errorf("first input_hash = %q, want %q", first.InputHash, wantFirst)
	}
	if second.InputHash == "" || second.InputHash == first.InputHash {
		t.Errorf("second input_hash = %q, want non-empty and distinct", second.InputHash)
	}

	// Model provenance: LLM door carries it, deterministic door is "".
	if first.Model != "provider/model-1" {
		t.Errorf("first model = %q, want provider/model-1", first.Model)
	}
	if second.Model != "" {
		t.Errorf("second model = %q, want empty (deterministic door)", second.Model)
	}

	// turn_no increments across same-session dispatches.
	if first.TurnNo != 1 {
		t.Errorf("first turn_no = %d, want 1", first.TurnNo)
	}
	if second.TurnNo != 2 {
		t.Errorf("second turn_no = %d, want 2", second.TurnNo)
	}

	// Outcome defaults to pending; corrected_agent empty.
	for i, e := range entries {
		if e.Outcome != "pending" {
			t.Errorf("row %d outcome = %q, want pending", i, e.Outcome)
		}
		if e.CorrectedAgent != "" {
			t.Errorf("row %d corrected_agent = %q, want empty", i, e.CorrectedAgent)
		}
	}
}

// TestRecordDispatch_ErrorScrubbed verifies the S4 error scrub: error text
// matching a key/token shape is replaced with "scrubbed", other long errors
// are capped at 200 chars.
func TestRecordDispatch_ErrorScrubbed(t *testing.T) {
	d, store := newPrivacyTestDispatcher(t)

	d.RecordDispatch("sess-err", "route_to_agent", "x",
		&DispatchResult{AgentID: "coder", Intent: &Intent{Type: "code"}}, false,
		errors.New("request failed: Bearer abc123def456ghijkl"))
	d.RecordDispatch("sess-err", "default_route", "y",
		&DispatchResult{AgentID: "generalist"}, false,
		errors.New(strings.Repeat("e", 500)))

	entries, err := store.QueryDispatchLogBySession("sess-err", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d rows, want 2", len(entries))
	}
	// ORDER BY id DESC: the 500-char error row is first, the scrubbed row second.
	if got := entries[1].Error; got != "scrubbed" {
		t.Errorf("bearer-token error stored as %q, want %q", got, "scrubbed")
	}
	if got := entries[0].Error; len(got) > 200 {
		t.Errorf("long error stored at %d chars, want <= 200", len(got))
	}
}

// TestRecordDispatch_NilStoreAndNilHasher verifies the preserved invariants:
// metricsStore nil => no panic and no row; hasher nil => input_hash "".
func TestRecordDispatch_NilStoreAndNilHasher(t *testing.T) {
	// Nil store: must not panic (multi-user-disabled path).
	nilStoreDispatcher := NewDispatcher(DispatcherConfig{})
	nilStoreDispatcher.RecordDispatch("sess-nil", "default_route", "raw text that must not go anywhere",
		&DispatchResult{AgentID: "generalist"}, false, nil)

	// Wired store, nil hasher: row written with empty input_hash.
	s, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	d := NewDispatcher(DispatcherConfig{})
	d.SetMetricsStore(s)
	d.RecordDispatch("sess-nohash", "default_route", "no hasher wired",
		&DispatchResult{AgentID: "generalist"}, false, nil)

	entries, err := s.QueryDispatchLogBySession("sess-nohash", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d rows, want 1", len(entries))
	}
	if entries[0].InputHash != "" {
		t.Errorf("input_hash = %q, want empty (nil hasher)", entries[0].InputHash)
	}
	if entries[0].InputSummary != "" {
		t.Errorf("input_summary = %q, want empty", entries[0].InputSummary)
	}
	if entries[0].TurnNo != 1 {
		t.Errorf("turn_no = %d, want 1", entries[0].TurnNo)
	}
}

// TestSetInputHasher_NilSafe verifies the setter's nil guard (CLAUDE.md
// setter practice): a typed-nil func must not disable an existing hasher
// and must not panic.
func TestSetInputHasher_NilSafe(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})
	d.SetInputHasher(nil) // must not panic
	if d.inputHasher != nil {
		t.Error("nil setter call overwrote nothing, but field changed")
	}
	d.SetInputHasher(func(string) string { return "h" })
	d.SetInputHasher(nil) // typed-nil must not clobber the wired hasher
	if d.inputHasher == nil {
		t.Error("typed-nil setter call clobbered a wired hasher")
	}
}

// TestRecordDispatch_MarginNULLRaw verifies the raw NULL (not 0) semantics
// for a non-Door-1 dispatch recorded through the dispatcher path.
func TestRecordDispatch_MarginNULLRaw(t *testing.T) {
	d, store := newPrivacyTestDispatcher(t)

	d.RecordDispatch("sess-margin", "default_route", "no margin here",
		&DispatchResult{AgentID: "generalist", Intent: &Intent{Type: "general"}}, false, nil)

	entries, err := store.QueryDispatchLogBySession("sess-margin", 10)
	if err != nil {
		t.Fatalf("QueryDispatchLogBySession: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d rows, want 1", len(entries))
	}
	if entries[0].Margin != nil {
		t.Errorf("margin = %v, want nil (NULL)", entries[0].Margin)
	}
}
