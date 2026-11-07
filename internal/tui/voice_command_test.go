package tui

// voice_command_test.go - leaf 13a T1 tests: /voice registration,
// help text, keymap collision pin.

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/tui/models"
)

// T1: /voice is registered as a builtin (autocomplete offers it) and
// IsBuiltin gates it into executeBuiltin.
func TestVoiceCommandRegistered(t *testing.T) {
	found := false
	for _, name := range sharedclient.BuiltinCommands() {
		if name == "voice" {
			found = true
		}
	}
	if !found {
		t.Fatal("/voice not registered in sharedclient.builtinCommands (autocomplete will not offer it)")
	}
	if !IsBuiltin("voice") {
		t.Fatal("IsBuiltin(voice) = false, want true")
	}
}

// T1: executeSync never falls into the unknown-command default for
// /voice (the TestEveryBuiltinCommandHasHandler contract, pinned
// explicitly for /voice here with a nil-RPC handler).
func TestVoiceCommandHandled(t *testing.T) {
	h := NewCommandHandler(nil, WithChatModelGetter(func() *models.ChatModel { return nil }))
	res := h.executeSync(&SlashCommand{Name: "voice"})
	if res == nil {
		t.Fatal("executeSync(/voice) = nil result")
	}
	if strings.HasPrefix(res.Output, "unknown command") {
		t.Fatalf("/voice registered but unhandled: %q", res.Output)
	}
}

// T1: help text exists for /voice, lowercase per the repo UI rule.
func TestVoiceCommandHelpText(t *testing.T) {
	h := NewCommandHandler(nil)
	res := h.executeSync(&SlashCommand{Name: "help", Args: []string{"voice"}})
	if res.IsError {
		t.Fatalf("/help voice = error: %q", res.Output)
	}
	if !strings.Contains(res.Output, "usage: /voice") {
		t.Fatalf("/help voice output missing usage line: %q", res.Output)
	}
	lower := strings.ToLower(res.Output)
	if lower != res.Output {
		t.Fatalf("/help voice output is not lowercase (UI rule):\n%s", res.Output)
	}
}

// T1: the general help listing mentions /voice.
func TestVoiceInGeneralHelp(t *testing.T) {
	h := NewCommandHandler(nil)
	res := h.executeSync(&SlashCommand{Name: "help"})
	if !strings.Contains(res.Output, "/voice") {
		t.Fatal("general help does not list /voice")
	}
}

// Self-verification: no keymap collision for voice. The TUI keymap
// registry (KeyMap) owns non-printable bindings; /voice is a slash
// command, never a KeyMap entry. This pins that assumption: the
// DefaultKeyMap keys must not contain a "voice" key binding.
func TestVoiceNoKeymapCollision(t *testing.T) {
	km := DefaultKeyMap()
	for _, b := range []interface{ Keys() []string }{km.Quit, km.Enter, km.Escape, km.Help, km.Command, km.ToggleTTS, km.ToggleMarkdown} {
		for _, k := range b.Keys() {
			if k == "voice" {
				t.Fatalf("keymap collision: binding uses key %q", k)
			}
		}
	}
}
