package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

// replyGuardTestHandler records every log record (with its attributes) so the
// guard WARN can be asserted structurally instead of by substring matching.
type replyGuardTestHandler struct {
	records []slog.Record
	attrs   []map[string]string
}

func (h *replyGuardTestHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *replyGuardTestHandler) Handle(_ context.Context, r slog.Record) error {
	attrMap := make(map[string]string, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrMap[a.Key] = a.Value.String()
		return true
	})
	h.records = append(h.records, r)
	h.attrs = append(h.attrs, attrMap)
	return nil
}

func (h *replyGuardTestHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *replyGuardTestHandler) WithGroup(string) slog.Handler      { return h }

// guardTestInputs are the three trigger classes the guard recognises, with the
// rule and matched-token each must report.
func guardTestInputs() []struct {
	name        string
	reply       string
	wantRule    string
	wantMatched string
} {
	return []struct {
		name        string
		reply       string
		wantRule    string
		wantMatched string
	}{
		{
			name:        "raw platform json names the first matching payload key",
			reply:       "{\n  \"status\": \"running\",\n  \"uptime_seconds\": 1840.8,\n  \"version\": \"1.0\"\n}",
			wantRule:    "raw_platform_json",
			wantMatched: `"status": "running"`,
		},
		{
			name:        "raw platform json names the uptime key when status is absent",
			reply:       "{\n  \"uptime_seconds\": 1840.8,\n  \"version\": \"1.0\"\n}",
			wantRule:    "raw_platform_json",
			wantMatched: `"uptime_seconds"`,
		},
		{
			name:        "agent roster names the header",
			reply:       "## Available Agents\n\n### Coder (`coder`)\n**Role**: executor\n\n*Total: 28 agents*",
			wantRule:    "agents_header",
			wantMatched: "## Available Agents",
		},
		{
			name:        "tool catalog names the totals line",
			reply:       "### Shell Tools\n\n- **shell**: Execute a shell command...\n\n*Total: 75 tools*",
			wantRule:    "tools_totals_line",
			wantMatched: "*Total: 75 tools*",
		},
	}
}

// TestApplyReplyGuardLoggedWarnsPerTriggerClass is the observability regression
// test: every trigger class must emit exactly one WARN naming the rule that
// matched, plus agent, intent, session/conversation id, and a bounded preview.
func TestApplyReplyGuardLoggedWarnsPerTriggerClass(t *testing.T) {
	ctx := replyGuardContext{
		Agent:          "coder",
		Intent:         "platform_status",
		SessionID:      "sess-abc123",
		ConversationID: "conv-xyz789",
	}
	for _, tc := range guardTestInputs() {
		t.Run(tc.name, func(t *testing.T) {
			handler := &replyGuardTestHandler{}
			logger := slog.New(handler)

			got := applyReplyGuardLogged(tc.reply, logger, ctx)

			if got == tc.reply {
				t.Fatalf("guard did not replace the machine-shaped reply")
			}
			if len(handler.records) != 1 {
				t.Fatalf("want exactly 1 log record, got %d", len(handler.records))
			}
			rec := handler.records[0]
			if rec.Level != slog.LevelWarn {
				t.Errorf("level = %v, want WARN", rec.Level)
			}
			if rec.Message != "reply guard replaced a machine-shaped reply" {
				t.Errorf("message = %q", rec.Message)
			}
			attrs := handler.attrs[0]
			checks := map[string]string{
				"rule":            tc.wantRule,
				"matched":         tc.wantMatched,
				"agent":           "coder",
				"intent":          "platform_status",
				"session_id":      "sess-abc123",
				"conversation_id": "conv-xyz789",
			}
			for key, want := range checks {
				if attrs[key] != want {
					t.Errorf("attr %s = %q, want %q (all: %v)", key, attrs[key], want, attrs)
				}
			}
			if attrs["preview"] == "" {
				t.Errorf("preview attr must be present (all: %v)", attrs)
			}
			if attrs["replaced_chars"] == "" {
				t.Errorf("replaced_chars attr must be present (all: %v)", attrs)
			}
			t.Logf("WARN attrs for %s: rule=%s matched=%s agent=%s intent=%s session=%s conversation=%s replaced_chars=%s preview=%q",
				tc.wantRule, attrs["rule"], attrs["matched"], attrs["agent"], attrs["intent"],
				attrs["session_id"], attrs["conversation_id"], attrs["replaced_chars"], attrs["preview"])
		})
	}
}

// TestApplyReplyGuardLoggedSilentWhenNotReplaced asserts the WARN is emitted on
// replacement only: prose, empty replies, and machine-shaped-but-prose replies
// (the 40% escape hatch) must not log.
func TestApplyReplyGuardLoggedSilentWhenNotReplaced(t *testing.T) {
	inputs := map[string]string{
		"prose":                    "I created water_reminder.py for you. Run it with python3.",
		"empty":                    "",
		"markdown answer":          "## how to run\n\n1. open terminal\n2. run the script",
		"json mention in prose":    "The status payload looks like {\"status\": \"running\"} but I need you to fix the parser instead of reading it back to you.",
		"prose with totals line":   "Here is a short summary of what I changed today, and it is mostly running text so it reads as an answer rather than a catalog dump of any kind at all.\n\n*Total: 3 tools*\n\nLet me know if you want the details of each one.",
		"totals line no keyword":   "*Total: 12 items*",
		"json without payload key": "{\"answer\": \"hello\"}",
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			handler := &replyGuardTestHandler{}
			got := applyReplyGuardLogged(in, slog.New(handler), replyGuardContext{})
			if got != in {
				t.Fatalf("reply must pass through unchanged, got %q", got)
			}
			if len(handler.records) != 0 {
				t.Fatalf("want no log records, got %d: %v", len(handler.records), handler.attrs)
			}
		})
	}
}

// TestReplyGuardPreviewBounded proves the WARN never carries the full payload:
// a ~12KB dump is truncated to the preview bound and the payload tail is
// absent from both the structured attr and the rendered text line.
func TestReplyGuardPreviewBounded(t *testing.T) {
	const sentinel = "TAILPAYLOAD_SENTINEL_9f3c"
	huge := "### Shell Tools\n\n*Total: 75 tools*\n\n" +
		strings.Repeat("- **tool**: does a thing with a long description\n", 250) +
		sentinel

	handler := &replyGuardTestHandler{}
	got := applyReplyGuardLogged(huge, slog.New(handler), replyGuardContext{Agent: "coder"})
	if got == huge {
		t.Fatalf("guard did not replace the oversized catalog")
	}
	if len(handler.records) != 1 {
		t.Fatalf("want exactly 1 log record, got %d", len(handler.records))
	}
	preview := handler.attrs[0]["preview"]
	if preview == "" {
		t.Fatalf("preview attr missing")
	}
	if runes := len([]rune(preview)); runes > replyGuardPreviewLimit+len(replyGuardPreviewSuffix) {
		t.Errorf("preview has %d runes, want <= %d", runes, replyGuardPreviewLimit+len(replyGuardPreviewSuffix))
	}
	if !strings.HasSuffix(preview, replyGuardPreviewSuffix) {
		t.Errorf("truncated preview must carry the marker, got %q", preview)
	}
	if strings.Contains(preview, sentinel) {
		t.Errorf("preview leaked the payload tail: %q", preview)
	}
	if strings.Contains(preview, "\n") {
		t.Errorf("preview must be single-line, got %q", preview)
	}
	// The token attribute is bounded too, so a one-line mega-dump cannot
	// smuggle a payload through "matched".
	matched := handler.attrs[0]["matched"]
	if runes := len([]rune(matched)); runes > replyGuardTokenLimit+3 {
		t.Errorf("matched token has %d runes, want <= %d", runes, replyGuardTokenLimit+3)
	}

	// Rendered form: exactly one line, still bounded, payload absent.
	var buf strings.Builder
	applyReplyGuardLogged(huge, slog.New(slog.NewTextHandler(&buf, nil)), replyGuardContext{Agent: "coder"})
	line := buf.String()
	t.Logf("rendered WARN line: %s", strings.TrimSpace(line))
	if strings.Count(strings.TrimRight(line, "\n"), "\n") != 0 {
		t.Errorf("guard WARN must render as one line, got:\n%s", line)
	}
	for _, want := range []string{
		"level=WARN",
		`msg="reply guard replaced a machine-shaped reply"`,
		"rule=tools_totals_line",
		"agent=coder",
		"preview=",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("rendered WARN missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, sentinel) {
		t.Errorf("rendered WARN leaked the payload tail:\n%s", line)
	}
}

// TestApplyReplyGuardLogsThroughProcessDefault covers the RunOnce seam
// (loop.go) without touching it: applyReplyGuard logs through slog.Default().
func TestApplyReplyGuardLogsThroughProcessDefault(t *testing.T) {
	prev := slog.Default()
	handler := &replyGuardTestHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	catalog := "### Shell Tools\n\n- **shell**: Execute a shell command...\n\n*Total: 75 tools*"
	if got := applyReplyGuard(catalog); got == catalog {
		t.Fatalf("applyReplyGuard let the catalog through")
	}
	if len(handler.records) != 1 {
		t.Fatalf("want exactly 1 WARN from the process default logger, got %d", len(handler.records))
	}
	if handler.attrs[0]["rule"] != "tools_totals_line" {
		t.Errorf("rule = %q, want tools_totals_line", handler.attrs[0]["rule"])
	}
	if handler.records[0].Level != slog.LevelWarn {
		t.Errorf("level = %v, want WARN", handler.records[0].Level)
	}

	// A second pass over the ALREADY-guarded reply must not log again: the
	// fallback text is genuine prose, so the two guard seams cannot
	// double-report one replacement.
	if got := applyReplyGuard(sanitizeCatalogReply(catalog)); len(handler.records) != 1 {
		t.Errorf("guarded text must not re-trigger a log, records = %d (reply %q)", len(handler.records), got)
	}
}

// TestApplyReplyGuardLoggedNilLogger asserts a nil logger falls back to the
// process default instead of panicking on the replacement path.
func TestApplyReplyGuardLoggedNilLogger(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })

	catalog := "### Shell Tools\n\n*Total: 75 tools*"
	if got := applyReplyGuardLogged(catalog, nil, replyGuardContext{}); got == catalog {
		t.Fatalf("nil logger must not skip the guard")
	}
}

// legacySanitizeCatalogReply is the pre-observability guard, reproduced
// verbatim from the previous revision of reply_guard.go. It exists so the
// behaviour-parity test below can prove this change is observability-only.
func legacySanitizeCatalogReply(reply string) string {
	if reply == "" {
		return reply
	}

	category := ""
	switch {
	case looksLikeRawPlatformJSON(reply):
		category = "status"
	case strings.Contains(reply, "## Available Agents"):
		category = "agents"
	case strings.Contains(reply, "*Total: ") &&
		(strings.Contains(reply, " tools*") || strings.Contains(reply, " agents*")):
		category = "tools"
	}
	if category == "" {
		return reply
	}

	if category != "agents" && proseLineRatio(reply) > 0.40 {
		return reply
	}

	return "i looked up the platform " + category + " information, but that isn't a useful answer on its own. ask me to do something specific — for example 'make me a program that ...' — and i'll get to work."
}

// TestReplyGuardBehaviourUnchanged pins the replacement behaviour against the
// legacy implementation for every shape the guard distinguishes.
func TestReplyGuardBehaviourUnchanged(t *testing.T) {
	inputs := []string{
		"",
		"I created water_reminder.py for you. Run it with python3.",
		"## how to run\n\n1. open terminal",
		"{\n  \"status\": \"running\",\n  \"uptime_seconds\": 1.5\n}",
		"{\"answer\": \"hello\"}",
		"### Shell Tools\n\n- **shell**: run it\n\n*Total: 75 tools*",
		"### Agent Tools\n\n- **agent_x**: does things\n\n*Total: 28 agents*",
		"## Available Agents\n\n### Coder (`coder`)\n**Role**: executor",
		"Sure! here are the specialists I can call on to help you today.\n\n## Available Agents\n\n### Coder (`coder`)\n**Role**: executor\n\n*Total: 28 agents*\n\nlet me know which one you'd like!",
		"Mostly prose explaining the change in detail so this line reads as an answer and not a catalog at all.\n\n- one bullet\n- two bullets\n\n*Total: 5 tools*\n\nLet me know if you want more.",
		"*Total: 12 items*",
	}
	ctx := replyGuardContext{Agent: "coder", Intent: "chat", SessionID: "s1", ConversationID: "c1"}
	for i, in := range inputs {
		handler := &replyGuardTestHandler{}
		got := applyReplyGuardLogged(in, slog.New(handler), ctx)
		want := legacySanitizeCatalogReply(in)
		if got != want {
			t.Errorf("input %d: guarded reply = %q, legacy = %q", i, got, want)
		}
		if s := sanitizeCatalogReply(in); s != want {
			t.Errorf("input %d: sanitizeCatalogReply = %q, legacy = %q", i, s, want)
		}
		wantLogged := want != in
		if gotLogged := len(handler.records) == 1; gotLogged != wantLogged {
			t.Errorf("input %d: logged = %v, replaced = %v (reply %q)", i, gotLogged, wantLogged, got)
		}
	}
}

// TestReplyGuardAttributionHelpers covers the nil-safe attribution used at the
// handler call site.
func TestReplyGuardAttributionHelpers(t *testing.T) {
	if got := replyGuardAgent("explicit", &DispatchResult{AgentID: "routed"}); got != "explicit" {
		t.Errorf("explicit agent override lost: %q", got)
	}
	if got := replyGuardAgent("", &DispatchResult{AgentID: "routed"}); got != "routed" {
		t.Errorf("dispatcher agent not used: %q", got)
	}
	if got := replyGuardAgent("", nil); got != "" {
		t.Errorf("nil result must yield empty agent, got %q", got)
	}
	if got := replyGuardIntent(&DispatchResult{Intent: &Intent{Type: "platform_status"}}); got != "platform_status" {
		t.Errorf("intent = %q, want platform_status", got)
	}
	if got := replyGuardIntent(&DispatchResult{}); got != "" {
		t.Errorf("nil intent must yield empty string, got %q", got)
	}
	if got := replyGuardIntent(nil); got != "" {
		t.Errorf("nil result must yield empty intent, got %q", got)
	}
}
