package tui

import (
	"fmt"
	s "strings"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
)

// CommandResult represents the result of executing a command.
type CommandResult struct {
	// Output is the text to display to the user.
	Output string
	// IsError indicates if the output is an error message.
	IsError bool
	// ClearConversation indicates the conversation should be cleared.
	ClearConversation bool
	// RetryLast indicates the last message should be retried.
	RetryLast bool
	// UndoLast indicates the last exchange should be removed.
	UndoLast bool
	// ToggleVimMode indicates vim mode should be toggled.
	ToggleVimMode bool
}

// CommandResultMsg is a bubbletea message containing a command result.
type CommandResultMsg struct {
	Result *CommandResult
}

// CommandHandler handles execution of slash commands.
type CommandHandler struct {
	rpc *RPCClient
	// getChatModel returns the current chat model for undo/retry operations.
	getChatModel func() *models.ChatModel
}

// CommandHandlerOption configures a CommandHandler.
type CommandHandlerOption func(*CommandHandler)

// WithChatModelGetter sets the chat model getter function.
func WithChatModelGetter(fn func() *models.ChatModel) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.getChatModel = fn
	}
}

// NewCommandHandler creates a new command handler with the given options.
func NewCommandHandler(rpc *RPCClient, opts ...CommandHandlerOption) *CommandHandler {
	h := &CommandHandler{
		rpc: rpc,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Execute executes a slash command and returns a bubbletea command.
func (h *CommandHandler) Execute(cmd *SlashCommand) tea.Cmd {
	return func() tea.Msg {
		result := h.executeSync(cmd)
		return CommandResultMsg{Result: result}
	}
}

// executeSync executes a command synchronously and returns the result.
func (h *CommandHandler) executeSync(cmd *SlashCommand) *CommandResult {
	if cmd == nil {
		return &CommandResult{
			Output:  "invalid command",
			IsError: true,
		}
	}

	// Check if this is a built-in command
	if IsBuiltin(cmd.Name) {
		return h.executeBuiltin(cmd)
	}

	// Not a built-in, treat as skill invocation (not yet implemented in full TUI)
	return &CommandResult{
		Output:  fmt.Sprintf("skill invocation not yet implemented: %s", cmd.Name),
		IsError: true,
	}
}

// executeBuiltin executes a built-in command.
func (h *CommandHandler) executeBuiltin(cmd *SlashCommand) *CommandResult {
	switch cmd.Name {
	case "help":
		return h.executeHelp(cmd.Args)
	case "new", "clear":
		return h.executeNew()
	case "retry":
		return h.executeRetry()
	case "undo":
		return h.executeUndo()
	case "usage":
		return h.executeUsage()
	case "stop":
		return h.executeStop()
	case "status":
		return h.executeStatus()
	case "vim":
		return h.executeVim()
	default:
		return &CommandResult{
			Output:  fmt.Sprintf("unknown command: %s", cmd.Name),
			IsError: true,
		}
	}
}

// executeHelp shows help for commands.
func (h *CommandHandler) executeHelp(args []string) *CommandResult {
	if len(args) > 0 {
		// Help for specific command
		return h.helpForCommand(args[0])
	}

	// General help
	var sb s.Builder
	sb.WriteString("available commands:\n\n")
	sb.WriteString("  /help [command]     show help for commands\n")
	sb.WriteString("  /new, /clear        start fresh conversation\n")
	sb.WriteString("  /retry              retry last response\n")
	sb.WriteString("  /undo               remove last exchange\n")
	sb.WriteString("  /usage              show token usage for session\n")
	sb.WriteString("  /stop               stop current session's work\n")
	sb.WriteString("  /status             show platform health status\n")
	sb.WriteString("  /vim                toggle vim-style keybindings\n")

	return &CommandResult{Output: sb.String()}
}

// helpForCommand shows help for a specific command.
func (h *CommandHandler) helpForCommand(name string) *CommandResult {
	helpTexts := map[string]string{
		"help": `usage: /help [command]

show help information. without arguments, lists all commands.
with a command name, shows detailed help for that command.`,

		"new": `usage: /new

start a fresh conversation. clears the current conversation history
and begins a new session. same as /clear.`,

		"clear": `usage: /clear

clear the current conversation. starts fresh with no history.
same as /new.`,

		"retry": `usage: /retry

retry the last response. removes the last assistant message and
re-sends the last user message to get a new response.`,

		"undo": `usage: /undo

remove the last exchange. removes both the last user message and
the last assistant response from the conversation.`,

		"usage": `usage: /usage

show token usage and cost statistics for the current session
and overall daemon totals.`,

		"stop": `usage: /stop

stop all work for the current session. stops any active agent workers
processing tasks for this session.`,

		"status": `usage: /status

show comprehensive platform health status including:
  - Agent workers: status, model, current tool
  - Running tasks: tasks in progress
  - Memory usage: memory store statistics
  - Daemon status: uptime, tokens used, budget used`,

		"vim": `usage: /vim

toggle vim-style keybindings in the chat input. when enabled,
use vim keys for navigation (h/j/k/l) and modal editing.`,
	}

	if text, ok := helpTexts[name]; ok {
		return &CommandResult{Output: text}
	}

	return &CommandResult{
		Output:  fmt.Sprintf("no help available for: %s", name),
		IsError: true,
	}
}

// executeNew starts a fresh conversation.
func (h *CommandHandler) executeNew() *CommandResult {
	return &CommandResult{
		Output:            "conversation cleared",
		ClearConversation: true,
	}
}

// executeRetry retries the last response.
func (h *CommandHandler) executeRetry() *CommandResult {
	if h.getChatModel == nil {
		return &CommandResult{
			Output:  "retry not available",
			IsError: true,
		}
	}

	chat := h.getChatModel()
	if chat == nil {
		return &CommandResult{
			Output:  "chat model not available",
			IsError: true,
		}
	}

	if !chat.RetryLast() {
		return &CommandResult{
			Output:  "no message to retry",
			IsError: true,
		}
	}

	// Return special result that triggers re-send
	// The app will handle re-sending the message
	return &CommandResult{
		Output:  "retrying last message...",
		RetryLast: true,
	}
}

// executeUndo removes the last exchange.
func (h *CommandHandler) executeUndo() *CommandResult {
	if h.getChatModel == nil {
		return &CommandResult{
			Output:  "undo not available",
			IsError: true,
		}
	}

	chat := h.getChatModel()
	if chat == nil {
		return &CommandResult{
			Output:  "chat model not available",
			IsError: true,
		}
	}

	if !chat.UndoLast() {
		return &CommandResult{
			Output:  "no exchange to undo",
			IsError: true,
		}
	}

	return &CommandResult{
		Output:   "last exchange removed",
		UndoLast: true,
	}
}

// executeUsage shows usage statistics aggregated from session and child tasks.
func (h *CommandHandler) executeUsage() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  "not connected to daemon",
			IsError: true,
		}
	}

	// Get daemon status for overall totals
	status, err := h.rpc.Status()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get daemon status: %v", err),
			IsError: true,
		}
	}

	var sb s.Builder
	sb.WriteString("usage statistics:\n\n")
	sb.WriteString("daemon totals:\n")
	sb.WriteString(fmt.Sprintf("  tokens used:     %d\n", status.TokensUsed))
	sb.WriteString(fmt.Sprintf("  tokens remaining: %d\n", status.TokensRemaining))
	sb.WriteString(fmt.Sprintf("  budget used:     $%.4f\n", status.BudgetUsed))
	sb.WriteString(fmt.Sprintf("  budget remaining: $%.4f\n", status.BudgetRemaining))

	sb.WriteString("\n")

	return &CommandResult{Output: sb.String()}
}

// executeStop stops all work for the current session.
func (h *CommandHandler) executeStop() *CommandResult {
	// The actual stop is handled by the app since it has session context
	// Return a signal that stop was requested
	return &CommandResult{
		Output: "stop requested - use ctrl+c to stop current work",
	}
}

// executeStatus shows comprehensive platform health status.
func (h *CommandHandler) executeStatus() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  "not connected to daemon",
			IsError: true,
		}
	}

	var sb s.Builder

	// Get daemon status
	status, err := h.rpc.Status()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get daemon status: %v", err),
			IsError: true,
		}
	}

	// Get agent workers
	workers, err := h.rpc.ListWorkers()
	if err != nil {
		workers = nil
	}

	// Get running tasks
	tasksResp, err := h.rpc.ListTasksExtended()
	tasks := tasksResp.Tasks

	// Build status output
	sb.WriteString("platform status:\n\n")

	// Daemon status
	sb.WriteString("daemon:\n")
	uptime := int(status.UptimeSeconds)
	hours := uptime / 3600
	mins := (uptime % 3600) / 60
	secs := uptime % 60
	sb.WriteString(fmt.Sprintf("  uptime:        %dh %dm %ds\n", hours, mins, secs))
	sb.WriteString(fmt.Sprintf("  model:         %s\n", coalesce(status.Model, status.DefaultModel, "(not set)")))
	sb.WriteString(fmt.Sprintf("  tokens used:   %d\n", status.TokensUsed))
	sb.WriteString(fmt.Sprintf("  budget used:   $%.4f\n", status.BudgetUsed))

	// Agent workers
	sb.WriteString("\nagent workers:\n")
	if workers == nil || len(workers.Workers) == 0 {
		sb.WriteString("  no active workers\n")
	} else {
		sb.WriteString(fmt.Sprintf("  %d active:\n", len(workers.Workers)))
		for _, w := range workers.Workers {
			id := truncate(w.ID, 8)
			tool := ""
			if w.CurrentTool != "" {
				tool = fmt.Sprintf(" [%s]", truncate(w.CurrentTool, 20))
			}
			sb.WriteString(fmt.Sprintf("    %s: %s%s\n", id, w.State, tool))
		}
	}

	// Running tasks
	sb.WriteString("\ntasks:\n")
	runningTasks := filterRunningTasks(tasks)
	if len(runningTasks) == 0 {
		sb.WriteString("  no running tasks\n")
	} else {
		sb.WriteString(fmt.Sprintf("  %d running:\n", len(runningTasks)))
		for _, t := range runningTasks {
			name := coalesce(t.Name, truncate(t.ID, 12), "unnamed")
			progress := ""
			if t.TotalJobs > 0 {
				progress = fmt.Sprintf(" [%d/%d]", t.CompletedJobs, t.TotalJobs)
			}
			sb.WriteString(fmt.Sprintf("    %s: %s%s\n", t.State, name, progress))
		}
	}

	return &CommandResult{Output: sb.String()}
}

// filterRunningTasks returns tasks that are in a running state.
func filterRunningTasks(tasks []types.TaskExtended) []types.TaskExtended {
	var running []types.TaskExtended
	for _, t := range tasks {
		switch t.State {
		case "planning", "executing", "processing", "pending":
			running = append(running, t)
		}
	}
	return running
}

// truncate truncates a string to maxLen, adding ... if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// coalesce returns the first non-empty string from the given list.
func coalesce(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	if len(strs) > 0 {
		return strs[len(strs)-1]
	}
	return ""
}

// executeVim toggles vim mode in the chat input.
func (h *CommandHandler) executeVim() *CommandResult {
	if h.getChatModel == nil {
		return &CommandResult{
			Output:  "vim toggle not available",
			IsError: true,
		}
	}

	chat := h.getChatModel()
	if chat == nil {
		return &CommandResult{
			Output:  "chat model not available",
			IsError: true,
		}
	}

	// Toggle vim mode
	chat.ToggleVim()

	// Check current vim state for feedback message
	vimState := chat.VimState()
	var mode string
	if vimState != nil && vimState.Enabled {
		mode = "enabled"
	} else {
		mode = "disabled"
	}

	return &CommandResult{
		Output:        fmt.Sprintf("vim mode %s", mode),
		ToggleVimMode: true,
	}
}
