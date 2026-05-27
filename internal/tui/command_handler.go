package tui

import (
	"encoding/json"
	"fmt"
	"strings"

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
	// SetProjectID requests the app to switch the current session's project.
	SetProjectID string
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

	// Try template invocation via RPC
	if h.rpc != nil && h.rpc.IsConnected() {
		result, err := h.rpc.InvokeTemplate(cmd.Name, cmd.Args)
		if err == nil {
			return &CommandResult{Output: result}
		}
		// If the error indicates template not found, fall through to the
		// unknown command error. Log other errors but don't block the user.
		if !isTemplateNotFoundError(err) {
			return &CommandResult{
				Output:  fmt.Sprintf("template invocation failed: %v", err),
				IsError: true,
			}
		}
	}

	// Not a built-in, skill, or template
	return &CommandResult{
		Output:  fmt.Sprintf("unknown command: /%s", cmd.Name),
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
	case CmdTasks:
		return h.executeTasks(cmd.Args)
	case "cancel":
		return h.executeCancel(cmd.Args)
	case "amend":
		return h.executeAmend(cmd.Args)
	case "interrupt":
		return h.executeInterrupt(cmd.Args)
	case "vim":
		return h.executeVim()
	case "diff":
		return h.executeDiff()
	case "model":
		return h.executeModel(cmd.Args)
	case "compact":
		return h.executeCompact()
	case "edit":
		return h.executeEdit(cmd.Args)
	case "plan":
		return h.executePlan()
	case "review":
		return h.executeReview()
	case "project":
		return h.executeProject(cmd.Args)
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
	var sb strings.Builder
	sb.WriteString("available commands:\n\n")
	sb.WriteString("  /help [command]     show help for commands\n")
	sb.WriteString("  /new, /clear        start fresh conversation\n")
	sb.WriteString("  /retry              retry last response\n")
	sb.WriteString("  /undo               remove last exchange\n")
	sb.WriteString("  /usage              show token usage for session\n")
	sb.WriteString("  /stop               stop current session's work\n")
	sb.WriteString("  /status             show platform health status\n")
	sb.WriteString("  /vim                toggle vim-style keybindings\n")
	sb.WriteString("  /cancel <id>        cancel a task by ID\n")
	sb.WriteString("  /amend <id> <type>  submit amendment to task\n")
	sb.WriteString("  /interrupt <id>     interrupt a running task\n")
	sb.WriteString("  /tasks              list all tasks\n")
	sb.WriteString("  /diff               show git diff of changes\n")
	sb.WriteString("  /model [name]       show or switch model\n")
	sb.WriteString("  /compact            compact conversation context\n")
	sb.WriteString("  /edit <path>        open file in system editor\n")
	sb.WriteString("  /plan               enter planning mode\n")
	sb.WriteString("  /review             review current changes\n")
	sb.WriteString("  /project [subcmd]   manage projects (list, set, add, sync, status)\n")

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

		"tasks": `usage: /tasks [state]

list tasks with their current status. optionally filter by state.

examples:
  /tasks              list all tasks
  /tasks running      list only running tasks
  /tasks pending      list only pending tasks`,

		"cancel": `usage: /cancel <task-id>

cancel a task by ID. stops any in-flight work for the task.

examples:
  /cancel task-123    cancel specific task
  /cancel             list recent active tasks`,

		"amend": `usage: /amend <task-id> <type> [content]

submit an amendment to modify a task in progress.

amendment types:
  inject_context  - add context to the task
  skip_step       - skip a step (requires step_id)
  add_step        - add a new step (requires description)
  reprioritize    - reorder steps (requires step_ids)
  change_agent    - reassign step to different agent

examples:
  /amend task-123 inject_context "skip testing"
  /amend task-123 skip_step step-456
  /amend task-123 add_step "write tests"`,

		"interrupt": `usage: /interrupt <task-id> [reason]

interrupt a running task. sends an interrupt signal to the task,
causing it to gracefully stop.

examples:
  /interrupt task-123          interrupt with default reason
  /interrupt task-123 wrong direction`,

		"diff": `usage: /diff

show git diff of the current working directory. displays both staged
and unstaged changes with file-level statistics.`,

		"model": `usage: /model [name]

show the current model or switch to a different one.
without arguments, displays the active model.

examples:
  /model              show current model
  /model gpt-4        switch to gpt-4`,

		"compact": `usage: /compact

compact the conversation context. reduces token usage by summarizing
older messages while preserving recent context and key information.`,

		"edit": `usage: /edit <file-path>

open a file in the system editor (uses $EDITOR, defaults to vi).

examples:
  /edit main.go       open main.go in editor
  /edit config.yaml   open config.yaml in editor`,

		"plan": `usage: /plan

enter planning mode. the next message you send will be treated as a
planning request, causing the agent to decompose the task into steps
before executing.`,

		"review": `usage: /review

review current changes in the working directory. shows a summary of
staged and unstaged git changes with file statistics.`,

		"project": `usage: /project [subcommand]

manage registered projects.

subcommands:
  (none)              show current project info
  list                list all registered projects
  set <name|id>       switch to a different project
  add <path|url>      register a new project
  sync                synchronize the current project (git pull)
  status              show git status of current project

examples:
  /project                        show project info
  /project list                   list all projects
  /project set myapp              switch to project named "myapp"
  /project add /home/user/myapp   add local project
  /project add https://github.com/org/repo.git
  /project sync                   sync current project
  /project status                 show project git status`,
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
		Output:    "retrying last message...",
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
			Output:  ErrNotConnected,
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

	var sb strings.Builder
	sb.WriteString("usage statistics:\n\n")
	sb.WriteString("daemon totals:\n")
	fmt.Fprintf(&sb, "  tokens used:     %d\n", status.TokensUsed)
	fmt.Fprintf(&sb, "  tokens remaining: %d\n", status.TokensRemaining)
	fmt.Fprintf(&sb, "  budget used:     $%.4f\n", status.BudgetUsed)
	fmt.Fprintf(&sb, "  budget remaining: $%.4f\n", status.BudgetRemaining)

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
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	var sb strings.Builder

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
	var tasks []types.TaskExtended
	if err == nil {
		tasks = tasksResp.Tasks
	}

	// Build status output
	sb.WriteString("platform status:\n\n")

	// Daemon status
	sb.WriteString("daemon:\n")
	uptime := int(status.UptimeSeconds)
	hours := uptime / 3600
	mins := (uptime % 3600) / 60
	secs := uptime % 60
	fmt.Fprintf(&sb, "  uptime:        %dh %dm %ds\n", hours, mins, secs)
	fmt.Fprintf(&sb, "  model:         %s\n", coalesce(status.Model, status.DefaultModel, "(not set)"))
	fmt.Fprintf(&sb, "  tokens used:   %d\n", status.TokensUsed)
	fmt.Fprintf(&sb, "  budget used:   $%.4f\n", status.BudgetUsed)

	// Agent workers
	sb.WriteString("\nagent workers:\n")
	if workers == nil || len(workers.Workers) == 0 {
		sb.WriteString("  no active workers\n")
	} else {
		fmt.Fprintf(&sb, "  %d active:\n", len(workers.Workers))
		for _, w := range workers.Workers {
			id := truncate(w.ID, 8)
			tool := ""
			if w.CurrentTool != "" {
				tool = fmt.Sprintf(" [%s]", truncate(w.CurrentTool, 20))
			}
			fmt.Fprintf(&sb, "    %s: %s%s\n", id, w.State, tool)
		}
	}

	// Running tasks
	sb.WriteString("\ntasks:\n")
	runningTasks := filterRunningTasks(tasks)
	if len(runningTasks) == 0 {
		sb.WriteString("  no running tasks\n")
	} else {
		fmt.Fprintf(&sb, "  %d running:\n", len(runningTasks))
		for _, t := range runningTasks {
			name := coalesce(t.Name, truncate(t.ID, 12), "unnamed")
			progress := ""
			if t.TotalJobs > 0 {
				progress = fmt.Sprintf(" [%d/%d]", t.CompletedJobs, t.TotalJobs)
			}
			fmt.Fprintf(&sb, "    %s: %s%s\n", t.State, name, progress)
		}
	}

	return &CommandResult{Output: sb.String()}
}

// filterRunningTasks returns tasks that are in a running state.
func filterRunningTasks(tasks []types.TaskExtended) []types.TaskExtended {
	var running []types.TaskExtended
	for _, t := range tasks {
		switch t.State {
		case StatePlanning, StateExecuting, StateProcessing, "pending":
			running = append(running, t)
		}
	}
	return running
}

// truncate truncates a string to maxLen, adding ... if truncated.
func truncate(str string, maxLen int) string {
	if len(str) <= maxLen {
		return str
	}
	if maxLen <= 3 {
		return str[:maxLen]
	}
	return str[:maxLen-3] + "..."
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

// executeTasks lists tasks with their status.
func (h *CommandHandler) executeTasks(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	// Get all tasks
	tasksResp, err := h.rpc.ListTasksExtended()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to list tasks: %v", err),
			IsError: true,
		}
	}
	tasks := tasksResp.Tasks

	if len(tasks) == 0 {
		return &CommandResult{Output: "no tasks"}
	}

	var sb strings.Builder
	sb.WriteString("tasks:\n\n")

	// Filter by state if provided
	stateFilter := ""
	if len(args) > 0 {
		stateFilter = args[0]
	}

	count := 0
	for _, t := range tasks {
		// Apply state filter if specified
		if stateFilter != "" && !strings.EqualFold(t.State, stateFilter) {
			continue
		}

		name := coalesce(t.Name, truncate(t.ID, 12), "unnamed")
		progress := ""
		if t.TotalJobs > 0 {
			progress = fmt.Sprintf(" [%d/%d]", t.CompletedJobs, t.TotalJobs)
		}
		fmt.Fprintf(&sb, "  %s: %s%s\n", t.State, name, progress)
		count++
	}

	if count == 0 && stateFilter != "" {
		return &CommandResult{Output: fmt.Sprintf("no tasks in state: %s", stateFilter)}
	}

	return &CommandResult{Output: sb.String()}
}

// executeCancel cancels a task by ID.
func (h *CommandHandler) executeCancel(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	if len(args) == 0 {
		return &CommandResult{
			Output:  "usage: /cancel <task-id> [reason]",
			IsError: true,
		}
	}

	taskID := args[0]

	// If no task ID provided, show recent active tasks
	if taskID == "" {
		return h.executeTasks([]string{StateExecuting, StatePlanning})
	}

	reason := ""
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}

	if err := h.rpc.CancelTask(taskID); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to cancel task: %v", err),
			IsError: true,
		}
	}

	msg := fmt.Sprintf("task %s cancelled", taskID)
	if reason != "" {
		msg += ": " + reason
	}

	return &CommandResult{Output: msg}
}

// executeAmend submits an amendment to a task.
// usage: /amend <task-id> <type> [content]
// types: inject_context, skip_step, add_step, reprioritize, change_agent
func (h *CommandHandler) executeAmend(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	if len(args) < 2 {
		return &CommandResult{
			Output:  "usage: /amend <task-id> <type> [content]\n\nsubmit an amendment to modify a task.\n\namendment types:\n  inject_context  - add context to the task\n  skip_step       - skip a step (requires step_id in content)\n  add_step        - add a new step (requires description in content)\n  reprioritize    - reorder steps (requires step_ids in content)\n  change_agent    - reassign step to different agent\n\nexamples:\n  /amend task-123 inject_context \"skip testing, go straight to deployment\"\n  /amend task-123 skip_step step-456\n  /amend task-123 add_step \"write unit tests\"",
			IsError: true,
		}
	}

	taskID := args[0]
	amendmentType := args[1]
	content := ""
	if len(args) > 2 {
		content = strings.Join(args[2:], " ")
	}

	// Validate amendment type
	validTypes := map[string]bool{
		"inject_context": true,
		"skip_step":      true,
		"add_step":       true,
		"reprioritize":   true,
		"change_agent":   true,
	}
	if !validTypes[amendmentType] {
		return &CommandResult{
			Output:  fmt.Sprintf("invalid amendment type: %s\n\nvalid types: inject_context, skip_step, add_step, reprioritize, change_agent", amendmentType),
			IsError: true,
		}
	}

	// Build amendment request and send via RPC bus publish
	amendmentReq := map[string]any{
		ParamTaskID: taskID,
		"type":      amendmentType,
		"content":   content,
	}

	result, err := h.rpc.Call("task.amend.submit", amendmentReq)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to submit amendment: %v", err),
			IsError: true,
		}
	}

	// Parse the response for confirmation
	var resp struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to parse amendment response: %v", err),
			IsError: true,
		}
	}

	msg := fmt.Sprintf("amendment submitted:\n  id: %s\n  task: %s\n  type: %s", resp.ID, taskID, amendmentType)
	if resp.Message != "" {
		msg += "\n  " + resp.Message
	}
	if content != "" {
		msg += fmt.Sprintf("\n  content: %s", content)
	}

	return &CommandResult{Output: msg}
}

// executeInterrupt triggers an interrupt token for a task.
// usage: /interrupt <task-id> [reason]
func (h *CommandHandler) executeInterrupt(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	if len(args) == 0 {
		return &CommandResult{
			Output:  "usage: /interrupt <task-id> [reason]",
			IsError: true,
		}
	}

	taskID := args[0]
	reason := "user_requested"
	if len(args) > 1 {
		reason = strings.Join(args[1:], " ")
	}

	// Interrupt is handled via task cancellation for now
	if err := h.rpc.CancelTask(taskID); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to interrupt task: %v", err),
			IsError: true,
		}
	}

	return &CommandResult{
		Output: fmt.Sprintf("task %s interrupted (reason: %s)", taskID, reason),
	}
}

// executeDiff shows a git diff of the current working directory.
func (h *CommandHandler) executeDiff() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	result, err := h.rpc.Call("shell.execute", map[string]any{
		"command": "git diff --stat && git diff",
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get diff: %v", err),
			IsError: true,
		}
	}

	var resp struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to parse diff response: %v", err),
			IsError: true,
		}
	}

	if resp.Error != "" {
		return &CommandResult{
			Output:  fmt.Sprintf("diff error: %s", resp.Error),
			IsError: true,
		}
	}

	if resp.Output == "" {
		return &CommandResult{Output: "no changes detected"}
	}

	return &CommandResult{Output: resp.Output}
}

// executeModel shows or switches the current model.
func (h *CommandHandler) executeModel(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	if len(args) == 0 {
		// Show current model
		status, err := h.rpc.Status()
		if err != nil {
			return &CommandResult{
				Output:  fmt.Sprintf("failed to get status: %v", err),
				IsError: true,
			}
		}
		model := coalesce(status.Model, status.DefaultModel, "(not set)")
		return &CommandResult{Output: fmt.Sprintf("current model: %s", model)}
	}

	// Switch model
	modelName := strings.Join(args, " ")
	result, err := h.rpc.Call("config.set", map[string]any{
		"key":   "llm.model",
		"value": modelName,
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to switch model: %v", err),
			IsError: true,
		}
	}

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{
			Output: fmt.Sprintf("model switched to: %s", modelName),
		}
	}

	if resp.Message != "" {
		return &CommandResult{Output: resp.Message}
	}
	return &CommandResult{Output: fmt.Sprintf("model switched to: %s", modelName)}
}

// executeCompact triggers context compaction for the current session.
func (h *CommandHandler) executeCompact() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	result, err := h.rpc.Call("session.compact", map[string]any{})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to compact context: %v", err),
			IsError: true,
		}
	}

	var resp struct {
		MessagesBefore int    `json:"messages_before"`
		MessagesAfter  int    `json:"messages_after"`
		TokensSaved    int    `json:"tokens_saved"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{Output: "context compacted"}
	}

	if resp.Message != "" {
		return &CommandResult{Output: resp.Message}
	}

	return &CommandResult{
		Output: fmt.Sprintf("context compacted: %d -> %d messages (%d tokens saved)",
			resp.MessagesBefore, resp.MessagesAfter, resp.TokensSaved),
	}
}

// executeEdit opens a file in the system editor.
func (h *CommandHandler) executeEdit(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	if len(args) == 0 {
		return &CommandResult{
			Output:  "usage: /edit <file-path>",
			IsError: true,
		}
	}

	filePath := args[0]
	result, err := h.rpc.Call("shell.execute", map[string]any{
		"command": fmt.Sprintf("${EDITOR:-vi} %s", filePath),
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to open editor: %v", err),
			IsError: true,
		}
	}

	var resp struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{Output: fmt.Sprintf("editing: %s", filePath)}
	}

	if resp.Error != "" {
		return &CommandResult{
			Output:  fmt.Sprintf("editor error: %s", resp.Error),
			IsError: true,
		}
	}

	return &CommandResult{Output: fmt.Sprintf("opened %s in editor", filePath)}
}

// executePlan enters planning mode for the current task.
func (h *CommandHandler) executePlan() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	result, err := h.rpc.Call("session.set_mode", map[string]any{
		"mode": "planning",
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to enter planning mode: %v", err),
			IsError: true,
		}
	}

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{Output: "planning mode activated"}
	}

	if resp.Message != "" {
		return &CommandResult{Output: resp.Message}
	}
	return &CommandResult{Output: "planning mode activated - next message will be treated as a planning request"}
}

// executeReview reviews current changes in the working directory.
func (h *CommandHandler) executeReview() *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	result, err := h.rpc.Call("shell.execute", map[string]any{
		"command": "git diff --stat && git diff --cached --stat",
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to review changes: %v", err),
			IsError: true,
		}
	}

	var resp struct {
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to parse review response: %v", err),
			IsError: true,
		}
	}

	if resp.Error != "" {
		return &CommandResult{
			Output:  fmt.Sprintf("review error: %s", resp.Error),
			IsError: true,
		}
	}

	if resp.Output == "" {
		return &CommandResult{Output: "no changes to review"}
	}

	return &CommandResult{Output: fmt.Sprintf("current changes:\n\n%s", resp.Output)}
}

// isTemplateNotFoundError checks whether an RPC error indicates that the
// template was not found (as opposed to a network or execution error).
func isTemplateNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "template not found") ||
		strings.Contains(msg, "template substitution failed")
}

// executeProject handles /project slash commands.
// Subcommands:
//
//	/project             - show current project info
//	/project list        - list all registered projects
//	/project set <name>  - switch to a different project
//	/project add <path>  - register a new project
//	/project sync        - sync current project
//	/project status      - show current project status
func (h *CommandHandler) executeProject(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	subcmd := ""
	if len(args) > 0 {
		subcmd = args[0]
	}

	switch subcmd {
	case "", "info":
		return h.executeProjectInfo()
	case "list", "ls":
		return h.executeProjectList()
	case "add", "register":
		return h.executeProjectAdd(args[1:])
	case "set":
		return h.executeProjectSet(args[1:])
	case "sync":
		return h.executeProjectSync()
	case "status":
		return h.executeProjectStatus()
	default:
		return &CommandResult{
			Output:  fmt.Sprintf("unknown project subcommand: %s\nuse: /project [list|set|add|sync|status]", subcmd),
			IsError: true,
		}
	}
}

// executeProjectInfo shows the current project info.
func (h *CommandHandler) executeProjectInfo() *CommandResult {
	// List projects and show summary
	projects, err := h.rpc.ListProjects()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get projects: %v", err),
			IsError: true,
		}
	}

	if len(projects.Projects) == 0 {
		return &CommandResult{Output: "no projects registered\nuse /project add <path|url> to add one"}
	}

	var sb strings.Builder
	sb.WriteString("registered projects:\n\n")
	for _, p := range projects.Projects {
		dirty := ""
		if p.Mode == "git" {
			// Try to get status for git projects
			status, err := h.rpc.ProjectStatus(p.ID)
			if err == nil && status.Dirty {
				dirty = " (dirty)"
			}
		}
		branch := ""
		if p.Branch != "" {
			branch = fmt.Sprintf(" branch:%s", p.Branch)
		}
		sb.WriteString(fmt.Sprintf("  %s  [%s]%s%s\n", p.Name, p.Mode, branch, dirty))
		if p.LocalPath != "" {
			sb.WriteString(fmt.Sprintf("    path: %s\n", p.LocalPath))
		}
		if p.GitURL != "" {
			sb.WriteString(fmt.Sprintf("    url:  %s\n", p.GitURL))
		}
	}
	return &CommandResult{Output: sb.String()}
}

// executeProjectList lists all registered projects.
func (h *CommandHandler) executeProjectList() *CommandResult {
	projects, err := h.rpc.ListProjects()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to list projects: %v", err),
			IsError: true,
		}
	}

	if len(projects.Projects) == 0 {
		return &CommandResult{Output: "no projects registered"}
	}

	var sb strings.Builder
	sb.WriteString("projects:\n\n")
	sb.WriteString(fmt.Sprintf("  %-20s %-8s %-16s %-10s %s\n", "NAME", "MODE", "BRANCH", "STATUS", "PATH"))
	for _, p := range projects.Projects {
		path := p.LocalPath
		if len(path) > 40 {
			path = "..." + path[len(path)-37:]
		}
		branch := p.Branch
		if branch == "" {
			branch = "-"
		}
		sb.WriteString(fmt.Sprintf("  %-20s %-8s %-16s %-10s %s\n", p.Name, p.Mode, branch, p.Status, path))
	}
	sb.WriteString(fmt.Sprintf("\n  total: %d projects", projects.Count))
	return &CommandResult{Output: sb.String()}
}

// executeProjectSet switches the current session to a different project.
func (h *CommandHandler) executeProjectSet(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  ErrNotConnected,
			IsError: true,
		}
	}

	if len(args) == 0 || args[0] == "" {
		return &CommandResult{
			Output:  "usage: /project set <name|id>",
			IsError: true,
		}
	}

	query := args[0]

	projects, err := h.rpc.ListProjects()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to list projects: %v", err),
			IsError: true,
		}
	}

	// Search for matching project by name (case-insensitive) or ID (exact)
	for _, p := range projects.Projects {
		if strings.EqualFold(p.Name, query) || p.ID == query {
			return &CommandResult{
				Output:       fmt.Sprintf("switching to project '%s'...", p.Name),
				SetProjectID: p.ID,
			}
		}
	}

	return &CommandResult{
		Output:  fmt.Sprintf("project '%s' not found", query),
		IsError: true,
	}
}

// executeProjectAdd registers a new project.
func (h *CommandHandler) executeProjectAdd(args []string) *CommandResult {
	if len(args) == 0 {
		return &CommandResult{
			Output:  "usage: /project add <path|url>",
			IsError: true,
		}
	}

	source := args[0]
	name := ""
	if len(args) > 1 {
		name = args[1]
	}

	// Derive name from source if not provided
	if name == "" {
		parts := strings.Split(strings.TrimRight(source, "/"), "/")
		name = parts[len(parts)-1]
		name = strings.TrimSuffix(name, ".git")
	}

	// Determine if source is a git URL or local path
	var gitURL, localPath string
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://") {
		gitURL = source
	} else {
		localPath = source
	}

	project, err := h.rpc.RegisterProject(name, gitURL, localPath)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to register project: %v", err),
			IsError: true,
		}
	}

	return &CommandResult{Output: fmt.Sprintf("registered project: %s (id: %s, mode: %s)", project.Name, project.ID, project.Mode)}
}

// executeProjectSync syncs the current project.
func (h *CommandHandler) executeProjectSync() *CommandResult {
	// Get projects list to find the current one
	projects, err := h.rpc.ListProjects()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get projects: %v", err),
			IsError: true,
		}
	}

	if len(projects.Projects) == 0 {
		return &CommandResult{Output: "no projects registered"}
	}

	// Sync the first active project (or all if multiple)
	var synced []string
	for _, p := range projects.Projects {
		if p.Mode == "git" && p.Status == "active" {
			if err := h.rpc.SyncProject(p.ID); err != nil {
				return &CommandResult{
					Output:  fmt.Sprintf("failed to sync project %s: %v", p.Name, err),
					IsError: true,
				}
			}
			synced = append(synced, p.Name)
		}
	}

	if len(synced) == 0 {
		return &CommandResult{Output: "no git projects to sync"}
	}

	return &CommandResult{Output: fmt.Sprintf("synced: %s", strings.Join(synced, ", "))}
}

// executeProjectStatus shows status of the current project.
func (h *CommandHandler) executeProjectStatus() *CommandResult {
	projects, err := h.rpc.ListProjects()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get projects: %v", err),
			IsError: true,
		}
	}

	if len(projects.Projects) == 0 {
		return &CommandResult{Output: "no projects registered"}
	}

	var sb strings.Builder
	for _, p := range projects.Projects {
		if p.Status != "active" {
			continue
		}
		sb.WriteString(fmt.Sprintf("project: %s (%s)\n", p.Name, p.Mode))
		sb.WriteString(fmt.Sprintf("  id:     %s\n", p.ID))
		if p.LocalPath != "" {
			sb.WriteString(fmt.Sprintf("  path:   %s\n", p.LocalPath))
		}
		if p.GitURL != "" {
			sb.WriteString(fmt.Sprintf("  url:    %s\n", p.GitURL))
		}

		if p.Mode == "git" {
			status, err := h.rpc.ProjectStatus(p.ID)
			if err != nil {
				sb.WriteString(fmt.Sprintf("  status: error: %v\n", err))
				continue
			}
			sb.WriteString(fmt.Sprintf("  branch: %s\n", status.Branch))
			sb.WriteString(fmt.Sprintf("  dirty:  %v\n", status.Dirty))
			if status.Ahead > 0 {
				sb.WriteString(fmt.Sprintf("  ahead:  %d commits\n", status.Ahead))
			}
			if status.Behind > 0 {
				sb.WriteString(fmt.Sprintf("  behind: %d commits\n", status.Behind))
			}
			if status.ModifiedFiles > 0 {
				sb.WriteString(fmt.Sprintf("  modified files: %d\n", status.ModifiedFiles))
			}
		}
		sb.WriteString("\n")
	}

	if sb.Len() == 0 {
		return &CommandResult{Output: "no active projects"}
	}

	return &CommandResult{Output: strings.TrimSpace(sb.String())}
}
