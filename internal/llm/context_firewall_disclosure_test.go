package llm

// 04-clutter TEST-GATE T4 companion (F11a): firewall truncation disclosure.
//
// Frozen defect: F-NEW-8 (MINOR, LIVE, REPORT-GUI-RERUN.md). Context
// firewall truncation is DOUBLE-logged and still silent to the user: 648
// truncation WARNs in one session, 324 of them the NEW "window-independent
// bound" line added by a6608719 alongside the pre-existing "to fit model
// window" line - every oversized system message logs two WARNs, and the
// user is never told their context was cut (F-ORCH-17 persists: the first
// turn of p1's fresh session truncated at messages=3).
//
// Contract (01-server-reply-hygiene T4): oversized-system truncation logs
// ONE WARN per truncation event (dedupe the double bound) AND discloses the
// truncation to the user (C1 routing_notice or a brief system note) - never
// silent. The bound itself (a6608719 safety improvement) must keep holding
// on every path; small messages stay byte-identical.
//
// RED today: one oversized system message through an enabled firewall
// produces TWO truncation WARNs (window-independent bound at the top of
// processMessages + the final safety net at the bottom) and no disclosure
// surface exists. GREEN after 04-clutter/01.

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// truncationLogCapture records every log record (concurrency-safe).
type truncationLogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

type truncationLogHandler struct{ capture *truncationLogCapture }

func (h *truncationLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *truncationLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.capture.mu.Lock()
	defer h.capture.mu.Unlock()
	h.capture.records = append(h.capture.records, r.Clone())
	return nil
}

func (h *truncationLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *truncationLogHandler) WithGroup(string) slog.Handler      { return h }

func (c *truncationLogCapture) truncationWarns() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []slog.Record
	for _, r := range c.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "truncated oversized system message") {
			out = append(out, r)
		}
	}
	return out
}

// TestFirewallTruncationDisclosed pins F-NEW-8: ONE WARN per truncation
// event and a user-visible disclosure surface. RED today: double WARN,
// no disclosure.
func TestFirewallTruncationDisclosed(t *testing.T) {
	// ~33 KB system message: over the 12 KB bound, under a 32k-token
	// window's hard limit, so the enabled-firewall path reaches BOTH
	// bound sites (the exact double-log shape from the rerun).
	huge := strings.Repeat("skill catalog line\n", 1800)

	t.Run("enabled_firewall_logs_one_warn_per_truncation", func(t *testing.T) {
		capture := &truncationLogCapture{}
		logger := slog.New(&truncationLogHandler{capture: capture})
		inner := &capturingChatter{}
		f := NewContextFirewall(inner, &ModelConfig{ContextLimit: 32768},
			ContextFirewallConfig{Enabled: true}, nil, logger, &HeuristicTokenizer{})

		msgs := []ChatMessage{
			{Role: RoleSystem, Content: huge},
			{Role: RoleUser, Content: "hello"},
		}
		if _, err := f.Chat(context.Background(), msgs); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		// The bound must still hold (a6608719 safety improvement).
		if len(inner.got) != 2 || len(inner.got[0].Content) > maxSystemMessageBytes+256 {
			t.Fatalf("oversized system message not bounded: %+v", inner.got)
		}
		warns := capture.truncationWarns()
		if len(warns) != 1 {
			var msgsDump []string
			for _, w := range warns {
				msgsDump = append(msgsDump, w.Message)
			}
			t.Fatalf("F-NEW-8: one oversized system message produced %d truncation WARNs, want exactly 1 "+
				"(dedupe the window-independent bound + final safety net double log): %v", len(warns), msgsDump)
		}
	})

	t.Run("inert_path_logs_one_warn_per_truncation", func(t *testing.T) {
		// The window-independent bound runs BEFORE the inertness
		// early-return; on an inert path it is the only bound - still
		// exactly one WARN, never zero (silent) and never two.
		capture := &truncationLogCapture{}
		logger := slog.New(&truncationLogHandler{capture: capture})
		inner := &capturingChatter{}
		f := NewContextFirewall(inner, &ModelConfig{ContextLimit: 0},
			ContextFirewallConfig{Enabled: true}, nil, logger, &HeuristicTokenizer{})

		if _, err := f.Chat(context.Background(), []ChatMessage{
			{Role: RoleSystem, Content: huge},
			{Role: RoleUser, Content: "hello"},
		}); err != nil {
			t.Fatalf("Chat inert: %v", err)
		}
		if warns := capture.truncationWarns(); len(warns) != 1 {
			t.Fatalf("inert-path truncation logged %d WARNs, want 1", len(warns))
		}
	})

	t.Run("truncation_is_disclosed_never_silent", func(t *testing.T) {
		// C1 additive disclosure surface: the firewall must expose the
		// truncation event for the turn funnel to carry as routing_notice
		// (or an equivalent brief system note). Asserted through
		// reflection so the gate compiles before 01 lands the seam; RED
		// until a disclosure surface exists AND reports the truncation.
		//
		// Accepted shapes (strictest reading of "never silent"):
		//   - a Stats() counter field naming system-message truncations, OR
		//   - a method returning the pending truncation disclosure text.
		inner := &capturingChatter{}
		f := NewContextFirewall(inner, &ModelConfig{ContextLimit: 32768},
			ContextFirewallConfig{Enabled: true}, nil, slogDiscardLLMLogger(), &HeuristicTokenizer{})
		if _, err := f.Chat(context.Background(), []ChatMessage{
			{Role: RoleSystem, Content: huge},
			{Role: RoleUser, Content: "hello"},
		}); err != nil {
			t.Fatalf("Chat: %v", err)
		}

		stats := reflect.ValueOf(f.Stats())
		counterFound := false
		for _, name := range []string{"SystemTruncations", "SystemMessageTruncations", "TruncationEvents", "OversizedSystemTruncations"} {
			field := stats.FieldByName(name)
			if field.IsValid() && field.Kind() == reflect.Uint64 {
				counterFound = true
				if field.Uint() == 0 {
					t.Errorf("F-NEW-8: truncation counter %s stayed 0 after a truncation - the event is invisible to the turn funnel", name)
				}
				break
			}
		}
		methodFound := false
		for _, name := range []string{"TruncationNotice", "PendingTruncationNotice", "TruncationDisclosure"} {
			m := reflect.ValueOf(f).MethodByName(name)
			if m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() >= 1 && m.Type().Out(0).Kind() == reflect.String {
				methodFound = true
				notice := m.Call(nil)[0].String()
				if strings.TrimSpace(notice) == "" {
					t.Errorf("F-NEW-8: %s() returned empty after a truncation - disclosure must never be silent", name)
				}
				break
			}
		}
		if !counterFound && !methodFound {
			t.Fatalf("F-NEW-8: no truncation disclosure surface on ContextFirewall " +
				"(want a Stats() truncation counter or a notice accessor the turn funnel can carry as C1 routing_notice); " +
				"truncation is silent to the user today")
		}
	})

	t.Run("small_system_messages_stay_byte_identical", func(t *testing.T) {
		// Regression guard (GREEN today): no truncation, no WARN, no
		// disclosure noise on ordinary turns.
		capture := &truncationLogCapture{}
		logger := slog.New(&truncationLogHandler{capture: capture})
		inner := &capturingChatter{}
		f := NewContextFirewall(inner, &ModelConfig{ContextLimit: 32768},
			ContextFirewallConfig{Enabled: true}, nil, logger, &HeuristicTokenizer{})
		const small = "You are a helpful assistant."
		if _, err := f.Chat(context.Background(), []ChatMessage{
			{Role: RoleSystem, Content: small},
			{Role: RoleUser, Content: "hello"},
		}); err != nil {
			t.Fatalf("Chat small: %v", err)
		}
		if len(inner.got) != 2 || inner.got[0].Content != small {
			t.Fatalf("small system message altered: %+v", inner.got)
		}
		if warns := capture.truncationWarns(); len(warns) != 0 {
			t.Errorf("small messages produced %d truncation WARNs, want 0", len(warns))
		}
	})
}

// slogDiscardLLMLogger is the package-local discard logger (internal/llm
// tests must not depend on internal/agent test helpers).
func slogDiscardLLMLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
