package agent

// executor_denied_path_test.go — issue #137 mechanism 2, the audit half:
// the fence denial line was UNATTRIBUTABLE.
//
// daemon.log capture from the rig (verbatim):
//
//	level=WARN msg="tool permission decision" agent=code-reviewer decision=deny
//	  action=file_read tool=file_read agent=code-reviewer groups=- rule_source=base_rule
//	  risk=SAFE reason="Path does not match any allowed path pattern"
//
// It never says WHICH path the reviewer tried or which working dir resolved
// it. A reviewer path-resolution bug then poisons the harness-cost /
// bus-trace / transcript checkers nondeterministically, with nothing in the
// log to attribute the denial to.
//
// Contract: every decision line carries the denied path (resolved against the
// turn's working dir) and the working dir that resolved it. Only filesystem
// actions carry the pair; non-path decisions keep the frozen C2 key set.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// TestDecisionLog_DeniedPathAndWorkdir pins the attribution pair on a DENY.
func TestDecisionLog_DeniedPathAndWorkdir(t *testing.T) {
	e, capture := newDecisionLogExecutor(t)
	dir := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)

	e.Execute(ctx, llm.ToolCall{
		ID:   "call_137",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "file_read",
			Arguments: `{"path": "secret.txt"}`,
		},
	})

	records := decisionRecords(capture.records)
	if len(records) != 1 {
		t.Fatalf("expected 1 decision record, got %d: %+v", len(records), capture.records)
	}
	rec := records[0]
	if rec.attrs["decision"] != "deny" {
		t.Fatalf("decision = %q, want deny", rec.attrs["decision"])
	}
	wantPath := filepath.Join(dir, "secret.txt")
	if got := rec.attrs["path"]; got != wantPath {
		t.Errorf("path = %q, want %q (the denied path resolved against the turn working dir)", got, wantPath)
	}
	if got := rec.attrs["workdir"]; got != dir {
		t.Errorf("workdir = %q, want %q", got, dir)
	}
}

// TestDecisionLog_DeniedPathAbsolute pins the absolute-path case: an absolute
// argument resolves to itself and the working dir is still named.
func TestDecisionLog_DeniedPathAbsolute(t *testing.T) {
	e, capture := newDecisionLogExecutor(t)
	dir := t.TempDir()
	ctx := tools.ContextWithWorkingDir(context.Background(), dir)

	e.Execute(ctx, llm.ToolCall{
		ID:   "call_137b",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "file_read",
			Arguments: `{"path": "/etc/shadow"}`,
		},
	})

	records := decisionRecords(capture.records)
	if len(records) != 1 {
		t.Fatalf("expected 1 decision record, got %d", len(records))
	}
	if got := records[0].attrs["path"]; got != "/etc/shadow" {
		t.Errorf("path = %q, want /etc/shadow", got)
	}
	if got := records[0].attrs["workdir"]; got != dir {
		t.Errorf("workdir = %q, want %q", got, dir)
	}
}

// TestDecisionLog_NonPathDecisionKeepsC2Shape pins the frozen key set for a
// decision about a tool call with no path argument: the C2 keys, no
// path/workdir padding.
func TestDecisionLog_NonPathDecisionKeepsC2Shape(t *testing.T) {
	capture := &decisionLogHandler{}
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("platform_status", "Platform status", func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]any{"status": "ok"}, nil
	}))
	checker := security.NewPermissionChecker(security.Config{AllowedPaths: []string{"/tmp"}})
	e := NewExecutor(registry, checker, WithExecutorLogger(slog.New(capture)))

	e.Execute(context.Background(), llm.ToolCall{
		ID:   "call_nopath",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "platform_status",
			Arguments: `{}`,
		},
	})

	records := decisionRecords(capture.records)
	if len(records) != 1 {
		t.Fatalf("expected 1 decision record, got %d: %+v", len(records), capture.records)
	}
	attrs := records[0].attrs
	if _, ok := attrs["path"]; ok {
		t.Errorf("non-path decision must not carry a path key: %v", attrs)
	}
	if _, ok := attrs["workdir"]; ok {
		t.Errorf("non-path decision must not carry a workdir key: %v", attrs)
	}
	for _, k := range []string{"decision", "action", "tool", "agent", "groups", "rule_source", "risk", "reason"} {
		if _, ok := attrs[k]; !ok {
			t.Errorf("missing C2 key %q (attrs: %v)", k, attrs)
		}
	}
	if len(attrs) != 8 {
		t.Errorf("key count = %d, want 8 (attrs: %v)", len(attrs), attrs)
	}
}

// TestDeniedPathResolvesRelative covers the resolver itself: relative paths
// resolve against the turn working dir, absolute paths pass through, and an
// absent working dir leaves a relative path unresolved (logged as "" so the
// caller can drop the attribution pair).
func TestDeniedPathResolvesRelative(t *testing.T) {
	e := NewExecutor(NewPlaceholderToolRegistry(), nil)
	cases := []struct {
		name    string
		args    map[string]any
		workdir string
		want    string
	}{
		{"relative resolves", map[string]any{"path": "notes.txt"}, "/repo", "/repo/notes.txt"},
		{"absolute passes through", map[string]any{"path": "/etc/hosts"}, "/repo", "/etc/hosts"},
		{"no working dir leaves relative unresolved", map[string]any{"path": "notes.txt"}, "", ""},
		{"path is not a string", map[string]any{"path": 42}, "/repo", ""},
		{"no path key", map[string]any{"other": "x"}, "/repo", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.deniedPath(tc.args, tc.workdir); got != tc.want {
				t.Errorf("deniedPath(%v, %q) = %q, want %q", tc.args, tc.workdir, got, tc.want)
			}
		})
	}
}
