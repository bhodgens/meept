package tui

// voice_command.go - the /voice slash command (local-leg leaf 13a, T1
// + T2 command half). Subcommands mirror the `meept voice local` CLI
// (leaf 08) for TUI/CLI parity:
//
//	/voice <employee> [label]   start a local session (default: /voice list)
//	/voice stop                 end the live session
//	/voice list                 show live calls (both legs)
//
// All state lives in the VoiceSessionModel (internal/tui/models/
// voice.go); this file only parses args and formats results. Audio
// rides the model's dialer seam (leaf 08 shim injected from cmd/meept)
// - no audio code here either.

import (
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/tui/models"
)

// voiceUsage is the /voice help text (lowercase per the repo UI rule).
const voiceUsage = `usage: /voice <employee> [label] | /voice stop | /voice list

start, stop, and inspect local voice sessions for this machine.
/voice eve starts a live call with employee eve (mic + speaker via
the leaf 08 audio shim when available); /voice stop ends it;
/voice list shows every live call across both legs.`

// SetVoiceSessionModel wires the shared VoiceSessionModel into the
// handler (App construction time). A nil model is valid: /voice
// reports it is unavailable (the same posture /skill uses when the
// lister is unwired).
func (h *CommandHandler) SetVoiceSessionModel(m *models.VoiceSessionModel) {
	if m != nil {
		h.voice = m
	}
}

// executeVoice dispatches the /voice subcommands (leaf 13a T1).
func (h *CommandHandler) executeVoice(args []string) *CommandResult {
	if h.voice == nil {
		return &CommandResult{
			Output:  "voice session unavailable (no voice model wired)",
			IsError: true,
		}
	}
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{Output: ErrNotConnected, IsError: true}
	}

	if len(args) == 0 {
		return &CommandResult{
			Output:  voiceUsage,
			IsError: true,
		}
	}

	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "stop":
		return h.voiceStop()
	case "list", "status":
		return h.voiceList()
	case "help":
		return &CommandResult{Output: voiceUsage}
	default:
		// Not a subcommand keyword: the first arg is the employee id
		// (/voice eve [label]); the rest joins as the session label.
		return h.voiceStart(sub, strings.Join(args[1:], " "))
	}
}

// voiceStart opens a local session for the employee.
func (h *CommandHandler) voiceStart(employee, label string) *CommandResult {
	if err := h.voice.Start(employee, label); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("voice: %v", err),
			IsError: true,
		}
	}
	return &CommandResult{
		Output: fmt.Sprintf("voice: call %s open with %s (leg: local)", h.voice.CallID(), h.voice.Employee()),
	}
}

// voiceStop ends the live session.
func (h *CommandHandler) voiceStop() *CommandResult {
	if !h.voice.Active() {
		return &CommandResult{Output: "voice: no live session"}
	}
	if err := h.voice.Stop(); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("voice: %v", err),
			IsError: true,
		}
	}
	return &CommandResult{Output: "voice: call ended."}
}

// voiceList renders the both-legs status table.
func (h *CommandHandler) voiceList() *CommandResult {
	out, err := h.voice.List()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("voice: %v", err),
			IsError: true,
		}
	}
	return &CommandResult{Output: out}
}
