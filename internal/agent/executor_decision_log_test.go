package agent

// executor_decision_log_test.go — T2 gate for agent-groups branch-01
// leaf 02 (docs/plans/20261030-agent-groups/01-permissions/02-decision-audit.md).
//
// Captures slog records through a custom handler and pins the C2 contract:
//
//	msg="tool permission decision"
//	keys: decision, action, tool, agent, groups, rule_source, risk, reason
//
// deny => WARN, allow => DEBUG (default deny-warn); level=all => every
// decision at INFO; level=off => suppressed. Kept at the executor seam —
// pkg/security stays pure.

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
)

// decisionLogHandler records every record's level, message, and attribute
// map so tests can assert the exact C2 shape.
type decisionLogHandler struct {
	records []decisionLogRecord
}

type decisionLogRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

func (h *decisionLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *decisionLogHandler) Handle(_ context.Context, r slog.Record) error {
	rec := decisionLogRecord{
		level: r.Level,
		msg:   r.Message,
		attrs: map[string]string{},
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.String()
		return true
	})
	h.records = append(h.records, rec)
	return nil
}

func (h *decisionLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *decisionLogHandler) WithGroup(_ string) slog.Handler      { return h }

// newDecisionLogExecutor builds an executor wired to a capturing logger
// with a security checker that denies /etc paths.
func newDecisionLogExecutor(t *testing.T) (*Executor, *decisionLogHandler) {
	t.Helper()
	capture := &decisionLogHandler{}
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "Read file", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"content": "x"}, nil
	}))
	checker := security.NewPermissionChecker(security.Config{
		AllowedPaths: []string{"/tmp"},
		BlockedPaths: []string{"/etc"},
	})
	executor := NewExecutor(registry, checker, WithExecutorLogger(slog.New(capture)))
	return executor, capture
}

func runToolCall(e *Executor, path string) {
	e.Execute(context.Background(), llm.ToolCall{
		ID:   "call_dl",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "file_read",
			Arguments: `{"path": "` + path + `"}`,
		},
	})
}

func decisionRecords(records []decisionLogRecord) []decisionLogRecord {
	var out []decisionLogRecord
	for _, r := range records {
		if r.msg == "tool permission decision" {
			out = append(out, r)
		}
	}
	return out
}

// TestDecisionLog_DenyWarnLevel pins the DEFAULT level behavior: a deny
// logs one WARN line with the exact C2 keys; an allow logs one DEBUG
// line. No other decision lines appear.
func TestDecisionLog_DenyWarnLevel(t *testing.T) {
	e, capture := newDecisionLogExecutor(t)
	// executor default level is "" == deny-warn

	runToolCall(e, "/etc/passwd") // deny (blocked path)
	runToolCall(e, "/tmp/ok.txt") // allow

	records := decisionRecords(capture.records)
	if len(records) != 2 {
		t.Fatalf("expected 2 decision records, got %d: %+v", len(records), capture.records)
	}

	deny := records[0]
	if deny.level != slog.LevelWarn {
		t.Errorf("deny level = %v, want WARN", deny.level)
	}
	if deny.msg != "tool permission decision" {
		t.Errorf("deny msg = %q, want %q", deny.msg, "tool permission decision")
	}
	if deny.attrs["decision"] != "deny" {
		t.Errorf("deny decision = %q, want deny", deny.attrs["decision"])
	}
	if deny.attrs["rule_source"] != "base_rule" {
		t.Errorf("deny rule_source = %q, want base_rule (blocked-path deny)", deny.attrs["rule_source"])
	}

	allow := records[1]
	if allow.level != slog.LevelDebug {
		t.Errorf("allow level = %v, want DEBUG", allow.level)
	}
	if allow.attrs["decision"] != "allow" {
		t.Errorf("allow decision = %q, want allow", allow.attrs["decision"])
	}
	if allow.attrs["rule_source"] != "base_rule" {
		t.Errorf("allow rule_source = %q, want base_rule", allow.attrs["rule_source"])
	}
}

// TestDecisionLog_C2Shape pins the frozen key set exactly: decision,
// action, tool, agent, groups, rule_source, risk, reason — no more, no
// fewer — and the groups "-" placeholder until leaf 04.
func TestDecisionLog_C2Shape(t *testing.T) {
	e, capture := newDecisionLogExecutor(t)

	runToolCall(e, "/etc/passwd")

	records := decisionRecords(capture.records)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	attrs := records[0].attrs

	wantKeys := []string{"decision", "action", "tool", "agent", "groups", "rule_source", "risk", "reason"}
	if len(attrs) != len(wantKeys) {
		t.Fatalf("key count = %d, want %d (attrs: %v)", len(attrs), len(wantKeys), attrs)
	}
	for _, k := range wantKeys {
		if _, ok := attrs[k]; !ok {
			t.Errorf("missing C2 key %q (attrs: %v)", k, attrs)
		}
	}
	if attrs["action"] != "file_read" {
		t.Errorf("action = %q, want file_read", attrs["action"])
	}
	if attrs["tool"] != "file_read" {
		t.Errorf("tool = %q, want file_read", attrs["tool"])
	}
	if attrs["groups"] != "-" {
		t.Errorf("groups = %q, want \"-\" placeholder (leaf 04 owns real groups)", attrs["groups"])
	}
	if attrs["risk"] == "" {
		t.Error("risk key must be present and non-empty")
	}
	if attrs["reason"] == "" {
		t.Error("reason key must be present and non-empty")
	}
}

// TestDecisionLog_AllLevel pins level=all: every decision logs at INFO.
func TestDecisionLog_AllLevel(t *testing.T) {
	e, capture := newDecisionLogExecutor(t)
	e.SetDecisionLogLevel(DecisionLogLevelAll)

	runToolCall(e, "/etc/passwd") // deny
	runToolCall(e, "/tmp/ok.txt") // allow

	records := decisionRecords(capture.records)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	for i, rec := range records {
		if rec.level != slog.LevelInfo {
			t.Errorf("record %d level = %v, want INFO (level=all)", i, rec.level)
		}
		if rec.msg != "tool permission decision" {
			t.Errorf("record %d msg = %q", i, rec.msg)
		}
	}
}

// TestDecisionLog_OffSuppresses pins level=off: no decision lines at all,
// while the permission check itself still denies (suppression is log-only).
func TestDecisionLog_OffSuppresses(t *testing.T) {
	e, capture := newDecisionLogExecutor(t)
	e.SetDecisionLogLevel(DecisionLogLevelOff)

	runToolCall(e, "/etc/passwd") // deny still enforced, just not logged
	runToolCall(e, "/tmp/ok.txt")

	if records := decisionRecords(capture.records); len(records) != 0 {
		t.Fatalf("expected 0 records at level=off, got %d: %+v", len(records), records)
	}
}

// TestDecisionLog_SetDecisionLogLevelInvalid pins setter hardening:
// unknown values are ignored (level unchanged), the empty string resets
// to the deny-warn default.
func TestDecisionLog_SetDecisionLogLevelInvalid(t *testing.T) {
	e, _ := newDecisionLogExecutor(t)

	e.SetDecisionLogLevel("sometimes")
	e.mu.RLock()
	got := e.decisionLogLevel
	e.mu.RUnlock()
	if got != "" {
		t.Fatalf("invalid level %q must be ignored, executor level = %q", "sometimes", got)
	}

	e.SetDecisionLogLevel(DecisionLogLevelAll)
	e.SetDecisionLogLevel("")
	e.mu.RLock()
	got = e.decisionLogLevel
	e.mu.RUnlock()
	if got != "" {
		t.Fatalf("empty level must reset to default (\"\"), got %q", got)
	}
}
