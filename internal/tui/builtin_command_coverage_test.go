package tui

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/tui/models"
)

// TestEveryBuiltinCommandHasHandler pins that every command registered in
// sharedclient.builtinCommands (what IsBuiltin gates on and autocomplete
// offers) is actually handled: executeSync must never fall into the
// "unknown command" default for a registered builtin. This is the /task
// regression (registered + documented but unhandled, returning
// "unknown command: task"). A nil-RPC handler returning "not connected to
// daemon" counts as handled; only the unknown-command default fails.
func TestEveryBuiltinCommandHasHandler(t *testing.T) {
	h := NewCommandHandler(nil, WithChatModelGetter(func() *models.ChatModel { return nil }))
	for _, name := range sharedclient.BuiltinCommands() {
		t.Run(name, func(t *testing.T) {
			res := h.executeSync(&SlashCommand{Name: name})
			if res == nil {
				t.Fatalf("executeSync(/%s) = nil result", name)
			}
			if strings.HasPrefix(res.Output, "unknown command") {
				t.Fatalf("/%s is registered in builtinCommands but unhandled: %q", name, res.Output)
			}
		})
	}
}

// TestTaskAliasMatchesTasks pins that /task and /tasks produce identical
// results (docs/reference/cli.md advertises /task as an alias of /tasks).
func TestTaskAliasMatchesTasks(t *testing.T) {
	h := NewCommandHandler(nil)
	args := []string{"running"}
	taskRes := h.executeSync(&SlashCommand{Name: "task", Args: args})
	tasksRes := h.executeSync(&SlashCommand{Name: "tasks", Args: args})
	if taskRes.Output != tasksRes.Output || taskRes.IsError != tasksRes.IsError {
		t.Fatalf("/task = {%q,%v}, /tasks = {%q,%v}; alias must match",
			taskRes.Output, taskRes.IsError, tasksRes.Output, tasksRes.IsError)
	}
	if strings.HasPrefix(taskRes.Output, "unknown command") {
		t.Fatalf("/task still unhandled: %q", taskRes.Output)
	}
}
