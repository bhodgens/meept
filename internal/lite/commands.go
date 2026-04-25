package lite

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// CommandResult represents the result of executing a command.
type CommandResult struct {
	// Output is the text to display to the user.
	Output string
	// IsError indicates if the output is an error message.
	IsError bool
	// ClearConversation indicates the conversation should be cleared.
	ClearConversation bool
	// SwitchSession contains the session ID to switch to, if any.
	SwitchSession string
	// RetryLast indicates the last message should be retried.
	RetryLast bool
	// UndoLast indicates the last exchange should be removed.
	UndoLast bool
	// SkillInvoke contains the skill name to invoke, if this is a skill command.
	SkillInvoke string
	// SkillArgs contains arguments for the skill invocation.
	SkillArgs []string
}

// CommandResultMsg is a bubbletea message containing a command result.
type CommandResultMsg struct {
	Result *CommandResult
}

// CommandHandler handles execution of slash commands.
type CommandHandler struct {
	// getModel returns the current model name.
	getModel func() string
	// setModel sets the current model.
	setModel func(string) error
	// getUsage returns usage statistics.
	getUsage func() *UsageStats
	// listSessions returns available sessions.
	listSessions func() ([]SessionInfo, error)
	// getSession returns the current session.
	getSession func() *SessionInfo
	// switchSession switches to a different session.
	switchSession func(id string) error
	// listTasks returns available tasks.
	listTasks func() ([]TaskInfo, error)
	// getTask returns task details by ID.
	getTask func(id string) (*TaskInfo, error)
	// listSkills returns available skill names for autocomplete.
	listSkills func() []string
}

// UsageStats contains token and cost statistics.
type UsageStats struct {
	TotalTokens      int
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	SessionTokens    int
	SessionCost      float64
}

// SessionInfo contains session information.
type SessionInfo struct {
	ID          string
	Name        string
	Description string
	IsCurrent   bool
	MessageCount int
}

// TaskInfo contains task information.
type TaskInfo struct {
	ID          string
	Name        string
	Status      string
	Progress    float64
	Description string
}

// CommandHandlerOption configures a CommandHandler.
type CommandHandlerOption func(*CommandHandler)

// WithModelGetter sets the model getter function.
func WithModelGetter(fn func() string) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.getModel = fn
	}
}

// WithModelSetter sets the model setter function.
func WithModelSetter(fn func(string) error) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.setModel = fn
	}
}

// WithUsageGetter sets the usage statistics getter.
func WithUsageGetter(fn func() *UsageStats) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.getUsage = fn
	}
}

// WithSessionLister sets the session listing function.
func WithSessionLister(fn func() ([]SessionInfo, error)) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.listSessions = fn
	}
}

// WithSessionGetter sets the current session getter.
func WithSessionGetter(fn func() *SessionInfo) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.getSession = fn
	}
}

// WithSessionSwitcher sets the session switching function.
func WithSessionSwitcher(fn func(string) error) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.switchSession = fn
	}
}

// WithTaskLister sets the task listing function.
func WithTaskLister(fn func() ([]TaskInfo, error)) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.listTasks = fn
	}
}

// WithTaskGetter sets the task getter function.
func WithTaskGetter(fn func(string) (*TaskInfo, error)) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.getTask = fn
	}
}

// WithSkillLister sets the skill listing function.
func WithSkillLister(fn func() []string) CommandHandlerOption {
	return func(h *CommandHandler) {
		h.listSkills = fn
	}
}

// NewCommandHandler creates a new command handler with the given options.
func NewCommandHandler(opts ...CommandHandlerOption) *CommandHandler {
	h := &CommandHandler{}
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

	// Not a built-in, treat as a skill invocation
	return &CommandResult{
		SkillInvoke: cmd.Name,
		SkillArgs:   cmd.Args,
	}
}

// executeBuiltin executes a built-in command.
func (h *CommandHandler) executeBuiltin(cmd *SlashCommand) *CommandResult {
	switch cmd.Name {
	case "help":
		return h.executeHelp(cmd.Args)
	case "new", "clear":
		return h.executeNew()
	case "model":
		return h.executeModel(cmd.Args)
	case "retry":
		return h.executeRetry()
	case "undo":
		return h.executeUndo()
	case "usage":
		return h.executeUsage()
	case "session":
		return h.executeSession(cmd.Args)
	case "task":
		return h.executeTask(cmd.Args)
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
	sb.WriteString("  /model [name]       show or change current model\n")
	sb.WriteString("  /retry              retry last response\n")
	sb.WriteString("  /undo               remove last exchange\n")
	sb.WriteString("  /usage              show token and cost statistics\n")
	sb.WriteString("  /session [name|id]  list or switch sessions\n")
	sb.WriteString("  /task [id]          list or view tasks\n")
	sb.WriteString("  /<skill-name>       invoke an installed skill\n")

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

		"model": `usage: /model [name]

without arguments, shows the current model.
with a model name, switches to that model for future messages.

examples:
  /model              show current model
  /model claude-3     switch to claude-3`,

		"retry": `usage: /retry

retry the last response. removes the last assistant message and
re-sends the last user message to get a new response.`,

		"undo": `usage: /undo

remove the last exchange. removes both the last user message and
the last assistant response from the conversation.`,

		"usage": `usage: /usage

show token usage and cost statistics for the current session
and overall totals.`,

		"session": `usage: /session [name|id]

without arguments, lists all available sessions.
with a name or id, switches to that session.

examples:
  /session            list all sessions
  /session main       switch to session named "main"
  /session abc123     switch to session with id "abc123"`,

		"task": `usage: /task [id]

without arguments, lists all tasks.
with an id, shows details for that specific task.

examples:
  /task               list all tasks
  /task task-123      show details for task-123`,
	}

	if text, ok := helpTexts[name]; ok {
		return &CommandResult{Output: text}
	}

	// Check if it might be a skill
	if h.listSkills != nil {
		skills := h.listSkills()
		for _, skill := range skills {
			if skill == name {
				return &CommandResult{
					Output: fmt.Sprintf("/%s is an installed skill. invoke it with arguments to use.\n\nexample: /%s <your prompt>", name, name),
				}
			}
		}
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

// executeModel shows or changes the current model.
func (h *CommandHandler) executeModel(args []string) *CommandResult {
	if len(args) == 0 {
		// Show current model
		if h.getModel == nil {
			return &CommandResult{
				Output:  "model information not available",
				IsError: true,
			}
		}
		model := h.getModel()
		if model == "" {
			model = "(default)"
		}
		return &CommandResult{
			Output: fmt.Sprintf("current model: %s", model),
		}
	}

	// Set model
	newModel := args[0]
	if h.setModel == nil {
		return &CommandResult{
			Output:  "model switching not available",
			IsError: true,
		}
	}

	if err := h.setModel(newModel); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to set model: %v", err),
			IsError: true,
		}
	}

	return &CommandResult{
		Output: fmt.Sprintf("model set to: %s", newModel),
	}
}

// executeRetry retries the last response.
func (h *CommandHandler) executeRetry() *CommandResult {
	return &CommandResult{
		Output:    "retrying last message...",
		RetryLast: true,
	}
}

// executeUndo removes the last exchange.
func (h *CommandHandler) executeUndo() *CommandResult {
	return &CommandResult{
		Output:   "last exchange removed",
		UndoLast: true,
	}
}

// executeUsage shows usage statistics.
func (h *CommandHandler) executeUsage() *CommandResult {
	if h.getUsage == nil {
		return &CommandResult{
			Output:  "usage statistics not available",
			IsError: true,
		}
	}

	stats := h.getUsage()
	if stats == nil {
		return &CommandResult{
			Output: "no usage data yet",
		}
	}

	var sb strings.Builder
	sb.WriteString("usage statistics:\n\n")
	sb.WriteString("session:\n")
	sb.WriteString(fmt.Sprintf("  tokens: %d\n", stats.SessionTokens))
	sb.WriteString(fmt.Sprintf("  cost:   $%.4f\n", stats.SessionCost))
	sb.WriteString("\ntotal:\n")
	sb.WriteString(fmt.Sprintf("  tokens: %d (prompt: %d, completion: %d)\n",
		stats.TotalTokens, stats.PromptTokens, stats.CompletionTokens))
	sb.WriteString(fmt.Sprintf("  cost:   $%.4f\n", stats.TotalCost))

	return &CommandResult{Output: sb.String()}
}

// executeSession lists or switches sessions.
func (h *CommandHandler) executeSession(args []string) *CommandResult {
	if len(args) == 0 {
		// List sessions
		return h.listSessionsResult()
	}

	// Switch to session
	target := strings.Join(args, " ")
	if h.switchSession == nil {
		return &CommandResult{
			Output:  "session switching not available",
			IsError: true,
		}
	}

	// Try to find session by name or ID
	if h.listSessions != nil {
		sessions, err := h.listSessions()
		if err == nil {
			for _, s := range sessions {
				if s.ID == target || s.Name == target {
					if err := h.switchSession(s.ID); err != nil {
						return &CommandResult{
							Output:  fmt.Sprintf("failed to switch session: %v", err),
							IsError: true,
						}
					}
					return &CommandResult{
						Output:        fmt.Sprintf("switched to session: %s", s.Name),
						SwitchSession: s.ID,
					}
				}
			}
		}
	}

	// Session not found
	return &CommandResult{
		Output:  fmt.Sprintf("session not found: %s", target),
		IsError: true,
	}
}

// listSessionsResult returns a formatted list of sessions.
func (h *CommandHandler) listSessionsResult() *CommandResult {
	if h.listSessions == nil {
		return &CommandResult{
			Output:  "session listing not available",
			IsError: true,
		}
	}

	sessions, err := h.listSessions()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to list sessions: %v", err),
			IsError: true,
		}
	}

	if len(sessions) == 0 {
		return &CommandResult{
			Output: "no sessions found",
		}
	}

	var sb strings.Builder
	sb.WriteString("sessions:\n\n")
	for _, s := range sessions {
		marker := "  "
		if s.IsCurrent {
			marker = "* "
		}
		name := s.Name
		if s.Description != "" {
			name = s.Description
		}
		sb.WriteString(fmt.Sprintf("%s%s (%d messages)\n", marker, name, s.MessageCount))
	}

	return &CommandResult{Output: sb.String()}
}

// executeTask lists or views tasks.
func (h *CommandHandler) executeTask(args []string) *CommandResult {
	if len(args) == 0 {
		// List tasks
		return h.listTasksResult()
	}

	// View specific task
	taskID := args[0]
	if h.getTask == nil {
		return &CommandResult{
			Output:  "task details not available",
			IsError: true,
		}
	}

	task, err := h.getTask(taskID)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("task not found: %s", taskID),
			IsError: true,
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("task: %s\n", task.Name))
	sb.WriteString(fmt.Sprintf("id:     %s\n", task.ID))
	sb.WriteString(fmt.Sprintf("status: %s\n", task.Status))
	if task.Progress > 0 {
		sb.WriteString(fmt.Sprintf("progress: %.0f%%\n", task.Progress*100))
	}
	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", task.Description))
	}

	return &CommandResult{Output: sb.String()}
}

// listTasksResult returns a formatted list of tasks.
func (h *CommandHandler) listTasksResult() *CommandResult {
	if h.listTasks == nil {
		return &CommandResult{
			Output:  "task listing not available",
			IsError: true,
		}
	}

	tasks, err := h.listTasks()
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to list tasks: %v", err),
			IsError: true,
		}
	}

	if len(tasks) == 0 {
		return &CommandResult{
			Output: "no tasks found",
		}
	}

	var sb strings.Builder
	sb.WriteString("tasks:\n\n")
	for _, t := range tasks {
		status := t.Status
		if t.Progress > 0 && t.Progress < 1 {
			status = fmt.Sprintf("%s (%.0f%%)", t.Status, t.Progress*100)
		}
		sb.WriteString(fmt.Sprintf("  %s: %s [%s]\n", t.ID, t.Name, status))
	}

	return &CommandResult{Output: sb.String()}
}

// GetCompletions returns command completions for the given prefix.
// This is useful for autocomplete functionality.
func (h *CommandHandler) GetCompletions(prefix string) []string {
	if !strings.HasPrefix(prefix, "/") {
		return nil
	}

	// Remove the leading slash
	prefix = prefix[1:]

	var completions []string

	// Add matching built-in commands
	for _, cmd := range BuiltinCommands() {
		if strings.HasPrefix(cmd, prefix) {
			completions = append(completions, "/"+cmd)
		}
	}

	// Add matching skills
	if h.listSkills != nil {
		for _, skill := range h.listSkills() {
			if strings.HasPrefix(skill, prefix) {
				completions = append(completions, "/"+skill)
			}
		}
	}

	return completions
}
