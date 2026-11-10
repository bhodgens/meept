package config

// 00T §5 gate tests (caller-authz plan, branch 01-identity): the frozen
// [voice] key `unapproved_caller_allowed_tools`
// (VoiceConfig.UnapprovedCallerAllowedTools []string).
//
// Message-tool name choice (gate report, task 6): the plan's candidate
// names note_message / leave_message / message DO NOT exist in
// internal/tools/builtin. Grepping every Name() string in the package
// yields exactly three registered message tools - send_agent_message,
// subagent_send_message, team_message - of which the daemon wires
// builtin.NewSendAgentMessageTool (internal/daemon/
// employee_message_tools.go) as THE message-leaving tool (the message is
// queued for the recipient employee's next turn - the secretary's
// "take a message" lane). This gate pins "send_agent_message"; leaf 01
// must grep the same registry and confirm the same name.
//
// RED style: undefined field UnapprovedCallerAllowedTools until leaf 01
// lands it (struct-literal + selector compile errors only).

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// callerGateMessageTool is the pinned REAL message-taking tool name (see
// file header for the grep evidence).
const callerGateMessageTool = "send_agent_message"

// TestVoiceUnapprovedCallerAllowedToolsDefault pins the DefaultConfig
// value verbatim from master.md: ["ledger_append", send_agent_message],
// in that order.
func TestVoiceUnapprovedCallerAllowedToolsDefault(t *testing.T) {
	cfg := DefaultConfig()
	got := cfg.Voice.UnapprovedCallerAllowedTools
	want := []string{"ledger_append", callerGateMessageTool}
	if !slices.Equal(got, want) {
		t.Errorf("voice.unapproved_caller_allowed_tools default = %q, want %q (master O3)", got, want)
	}
}

// TestUnapprovedCallerAllowedToolsOmittedKeepsDefault pins the load with
// a [voice] table that omits the key: unmarshal runs ONTO DefaultConfig,
// so the default survives (same inheritance rule as the messaging block).
func TestUnapprovedCallerAllowedToolsOmittedKeepsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := "[voice]\nenabled = true\n"
	//nolint:gosec // test fixture
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"ledger_append", callerGateMessageTool}
	if !slices.Equal(cfg.Voice.UnapprovedCallerAllowedTools, want) {
		t.Errorf("omitted key loaded as %q, want default %q", cfg.Voice.UnapprovedCallerAllowedTools, want)
	}
}

// TestUnapprovedCallerAllowedToolsEmptyNormalizesToDefault pins the
// master rule "normalize at load (empty -> default)": an explicitly
// empty list must not ship zero message rights to unknown callers - the
// load boundary restores the default.
func TestUnapprovedCallerAllowedToolsEmptyNormalizesToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := "[voice]\nenabled = true\nunapproved_caller_allowed_tools = []\n"
	//nolint:gosec // test fixture
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"ledger_append", callerGateMessageTool}
	if !slices.Equal(cfg.Voice.UnapprovedCallerAllowedTools, want) {
		t.Errorf("explicit empty list normalized to %q, want %q (empty -> default at the load boundary)",
			cfg.Voice.UnapprovedCallerAllowedTools, want)
	}
}

// TestUnapprovedCallerAllowedToolsExplicitOverride pins the override
// posture - and the review-checklist rule "normalization does not
// reorder user lists": an explicit non-empty list is carried through
// byte-order, no defaults appended.
func TestUnapprovedCallerAllowedToolsExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := "[voice]\nenabled = true\nunapproved_caller_allowed_tools = [\"ledger_append\", \"reply_to_user\"]\n"
	//nolint:gosec // test fixture
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"ledger_append", "reply_to_user"}
	if !slices.Equal(cfg.Voice.UnapprovedCallerAllowedTools, want) {
		t.Errorf("explicit list = %q, want exactly %q in user order (no reorder, no default injection)",
			cfg.Voice.UnapprovedCallerAllowedTools, want)
	}
}

// TestUnapprovedCallerAllowedToolsWireKey pins the key spelling on both
// wire formats (json unapproved_caller_allowed_tools / toml same), the
// sibling-key tag posture of the rest of VoiceConfig.
func TestUnapprovedCallerAllowedToolsWireKey(t *testing.T) {
	cfg := DefaultConfig()
	doc := `{"voice": {"unapproved_caller_allowed_tools": ["ledger_append"]}}`
	if err := UnmarshalJSON5([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !slices.Equal(cfg.Voice.UnapprovedCallerAllowedTools, []string{"ledger_append"}) {
		t.Errorf("json key unapproved_caller_allowed_tools did not bind: %q", cfg.Voice.UnapprovedCallerAllowedTools)
	}
}
