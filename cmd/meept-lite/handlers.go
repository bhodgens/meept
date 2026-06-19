package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/sharedclient"
)

// CommandHandler handles slash command execution.
type CommandHandler struct {
	tui *TUI
}

// NewCommandHandler creates a new command handler bound to a TUI instance.
func NewCommandHandler(tui *TUI) *CommandHandler {
	return &CommandHandler{tui: tui}
}

// Handle dispatches a parsed slash command to the appropriate handler.
func (h *CommandHandler) Handle(ctx context.Context, cmd *sharedclient.SlashCommand) {
	switch cmd.Name {
	case "help":
		h.handleHelp(cmd)
	case "clear":
		h.tui.scrollback = h.tui.scrollback[:0]
		h.addScrollback("scrollback cleared")
	case "new":
		h.handleNew(ctx)
	case "retry":
		h.handleRetry()
	case "undo":
		h.handleUndo()
	case "usage":
		h.handleUsage(ctx)
	case "stop":
		h.handleStop(ctx)
	case "status":
		h.handleStatus(ctx)
	case "session":
		h.handleSession(ctx, cmd)
	case "tasks":
		h.handleTasks()
	case "cancel":
		h.handleCancel(ctx, cmd)
	case "amend":
		h.handleAmend(ctx, cmd)
	case "interrupt":
		h.handleInterrupt(ctx, cmd)
	default:
		h.addScrollback(fmt.Sprintf("/%s is a built-in command but not yet implemented", cmd.Name))
	}
	h.tui.render()
}

// -- Simple helpers --------------------------------------------------------

func (h *CommandHandler) addScrollback(line string) {
	h.tui.scrollback = append(h.tui.scrollback, line)
	if len(h.tui.scrollback) > 10000 {
		h.tui.scrollback = h.tui.scrollback[1000:]
	}
}

func (h *CommandHandler) addError(err error) {
	h.addScrollback(fmt.Sprintf("error: %v", err))
}

func (h *CommandHandler) addSectionHeader(title string) {
	h.addScrollback("")
	h.addScrollback(strings.Repeat("-", 50))
	h.addScrollback(title)
	h.addScrollback(strings.Repeat("-", 50))
}

func formatDuration(seconds float64) string {
	if seconds <= 0 {
		return "n/a"
	}
	d := int(seconds)
	days := d / 86400
	hours := (d % 86400) / 3600
	minutes := (d % 3600) / 60
	secs := d % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", secs))
	}
	return strings.Join(parts, " ")
}

// -- Command implementations -----------------------------------------------

func (h *CommandHandler) handleHelp(cmd *sharedclient.SlashCommand) {
	if len(cmd.Args) > 0 {
		target := cmd.Args[0]
		h.addSectionHeader(fmt.Sprintf("/%s help", target))
		switch target {
		case "retry":
			h.addScrollback("retry the last response from the daemon")
		case "undo":
			h.addScrollback("remove the last user-message and response exchange from scrollback")
		case "usage":
			h.addScrollback("display token usage and budget information from daemon status")
		case "stop":
			h.addScrollback("stop any currently active work on the current session")
		case "status":
			h.addScrollback("show daemon health, uptime, model, and resource usage")
		case "session":
			h.addScrollback("session management: /session list, /session create <name>,")
			h.addScrollback("  /session switch <name|id>, /session delete <name|id>")
		case "tasks":
			h.addScrollback("list all tasks and their current state")
		case "cancel":
			h.addScrollback("cancel a running or pending task")
		case "amend":
			h.addScrollback("amend a task's name, description or state: /amend <id> <field> [value]")
		case "interrupt":
			h.addScrollback("interrupt a running task with an optional reason: /interrupt <id> [reason]")
		default:
			h.addScrollback(fmt.Sprintf("no detailed help available for /%s", target))
		}
		return
	}

	h.addScrollback("=== meept-lite slash commands ===")
	h.addScrollback("")
	h.addScrollback("chat:")
	h.addScrollback("  /help [command]  show help (or for a specific command)")
	h.addScrollback("  /new              start a fresh conversation")
	h.addScrollback("  /clear            clear scrollback")
	h.addScrollback("  /retry            retry last response")
	h.addScrollback("  /undo             remove last message/response exchange")
	h.addScrollback("  /usage            show token usage and budget")
	h.addScrollback("  /stop             stop current session work")
	h.addScrollback("  /status           show daemon health status")
	h.addScrollback("")
	h.addScrollback("sessions:")
	h.addScrollback("  /session list              list all sessions")
	h.addScrollback("  /session create <name>     create a new session")
	h.addScrollback("  /session switch <id>       switch to a session")
	h.addScrollback("  /session delete <id>       delete a session")
	h.addScrollback("")
	h.addScrollback("tasks:")
	h.addScrollback("  /tasks             list all tasks")
	h.addScrollback("  /cancel <id>       cancel a task")
	h.addScrollback("  /amend <id> <f> [v] amend a task field")
	h.addScrollback("  /interrupt <id> [r] interrupt a task with a reason")
}

func (h *CommandHandler) handleNew(ctx context.Context) {
	h.tui.scrollback = h.tui.scrollback[:0]
	// Create a new session on the daemon side
	if ctx != nil {
		name := h.tui.sessionMgr.GetSessionName() + " (copy)"
		sess, err := h.tui.client.CreateSession(name)
		if err == nil {
			h.tui.sessionMgr.SetSession(sess)
		}
	}
	h.addScrollback("conversation started anew")
}

func (h *CommandHandler) handleRetry() {
	// Scan scrollback for the last "you:" line
	lastMsg := ""
	for i := len(h.tui.scrollback) - 1; i >= 0; i-- {
		line := h.tui.scrollback[i]
		if strings.HasPrefix(line, "you: ") {
			lastMsg = line[5:]
			break
		}
	}
	if lastMsg == "" {
		h.addScrollback("no previous message to retry")
		return
	}
	h.addScrollback("retrying...")
	h.tui.sendChatMessage(lastMsg)
}

func (h *CommandHandler) handleUndo() {
	// Scan from bottom up: skip the last response, then skip the last "you: X" line
	removed := false
	newScroll := make([]string, 0, len(h.tui.scrollback))
	for i := len(h.tui.scrollback) - 1; i >= 0; i-- {
		line := h.tui.scrollback[i]
		// Skip last meept response or error response
		if !removed && (strings.HasPrefix(line, "meept: ") || strings.HasPrefix(line, "error: ")) {
			removed = true
			continue
		}
		// Skip the user message
		if removed && strings.HasPrefix(line, "you: ") {
			continue
		}
		newScroll = append(newScroll, line)
	}
	if !removed || len(newScroll) == len(h.tui.scrollback) {
		h.addScrollback("nothing to undo")
		return
	}
	// Reverse since we built it backwards
	rev := make([]string, len(newScroll))
	for i := range newScroll {
		rev[i] = newScroll[len(newScroll)-1-i]
	}
	h.tui.scrollback = rev
	h.addScrollback("last exchange removed")
}

func (h *CommandHandler) handleUsage(_ context.Context) {
	resp, err := h.tui.client.Status()
	if err != nil {
		h.addError(err)
		return
	}
	h.addSectionHeader("token usage")
	h.addScrollback(fmt.Sprintf("  model:            %s", resp.Model))
	h.addScrollback(fmt.Sprintf("  default model:    %s", resp.DefaultModel))
	h.addScrollback(fmt.Sprintf("  tokens used:      %d", resp.TokensUsed))
	h.addScrollback(fmt.Sprintf("  tokens remaining: %d", resp.TokensRemaining))
	h.addScrollback(fmt.Sprintf("  budget used:      %.2f", resp.BudgetUsed))
	h.addScrollback(fmt.Sprintf("  budget remaining: %.2f", resp.BudgetRemaining))
}

func (h *CommandHandler) handleStop(_ context.Context) {
	sess := h.tui.sessionMgr.GetCurrentSession()
	if sess == nil {
		h.addScrollback("no current session to stop")
		return
	}
	resp, err := h.tui.client.StopSession(sess.ID)
	if err != nil {
		h.addError(err)
		return
	}
	h.addScrollback(fmt.Sprintf("session stopped: %s (workers: %v)",
		resp.SessionID, resp.WorkersStopped))
}

func (h *CommandHandler) handleStatus(_ context.Context) {
	resp, err := h.tui.client.Status()
	if err != nil {
		h.addError(err)
		return
	}
	h.addSectionHeader("daemon status")
	h.addScrollback(fmt.Sprintf("  status:          %s", resp.Status))
	h.addScrollback(fmt.Sprintf("  model:           %s", resp.Model))
	h.addScrollback(fmt.Sprintf("  default model:   %s", resp.DefaultModel))
	h.addScrollback(fmt.Sprintf("  uptime:          %s", formatDuration(resp.UptimeSeconds)))
	h.addScrollback(fmt.Sprintf("  subscribers:     %d", resp.BusSubscribers))
	h.addScrollback(fmt.Sprintf("  methods:         %d", len(resp.RegisteredMethods)))
}

func (h *CommandHandler) handleSession(_ context.Context, cmd *sharedclient.SlashCommand) {
	if len(cmd.Args) == 0 {
		h.addScrollback("usage: /session list|create <name>|switch <id>|delete <id>")
		return
	}

	sub := cmd.Args[0]
	args := cmd.Args[1:]

	switch sub {
	case "list":
		h.handleSessionList(args)
	case "create":
		h.handleSessionCreate(args)
	case "switch":
		h.handleSessionSwitch(args)
	case "delete", "del":
		h.handleSessionDelete(args)
	default:
		h.addScrollback(fmt.Sprintf("unknown session subcommand: /session %s", sub))
	}
}

func (h *CommandHandler) handleSessionList(args []string) {
	resp, err := h.tui.client.ListSessions()
	if err != nil {
		h.addError(err)
		return
	}
	h.addSectionHeader("sessions")
	if len(resp.Sessions) == 0 {
		h.addScrollback("  (no sessions)")
		return
	}

	currentName := h.tui.sessionMgr.GetSessionName()
	currentID := ""
	if s := h.tui.sessionMgr.GetCurrentSession(); s != nil {
		currentID = s.ID
	}

	for i, s := range resp.Sessions {
		marker := "  "
		displayName := s.Name
		if s.Description != "" {
			displayName = s.Description
		}
		if displayName == currentName || s.ID == currentID {
			marker = "> "
		}
		h.addScrollback(fmt.Sprintf("%s[%d] %s (%s)", marker, i, displayName, s.ID))
	}
}

func (h *CommandHandler) handleSessionCreate(args []string) {
	if len(args) == 0 {
		h.addScrollback("usage: /session create <name>")
		return
	}
	name := strings.Join(args, " ")
	sess, err := h.tui.client.CreateSession(name)
	if err != nil {
		h.addError(err)
		return
	}
	h.tui.sessionMgr.SetSession(sess)
	h.tui.prompt.SetSessionName(sess.Name)
	h.addScrollback(fmt.Sprintf("created and switched to session: %s (%s)", sess.Name, sess.ID))
}

func (h *CommandHandler) handleSessionSwitch(args []string) {
	if len(args) == 0 {
		h.addScrollback("usage: /session switch <name|id>")
		return
	}
	identifier := strings.Join(args, " ")
	if err := h.tui.sessionMgr.SwitchSession(context.TODO(), identifier); err != nil {
		h.addError(err)
		return
	}
	sess := h.tui.sessionMgr.GetCurrentSession()
	h.tui.prompt.SetSessionName(h.tui.sessionMgr.GetSessionName())
	if sess != nil {
		h.addScrollback(fmt.Sprintf("switched to session: %s (%s)", h.tui.sessionMgr.GetSessionName(), sess.ID))
	} else {
		h.addScrollback(fmt.Sprintf("switched to session: %s", identifier))
	}
}

func (h *CommandHandler) handleSessionDelete(args []string) {
	if len(args) == 0 {
		h.addScrollback("usage: /session delete <name|id>")
		return
	}
	identifier := strings.Join(args, " ")

	// Protect against deleting current session
	sess := h.tui.sessionMgr.GetCurrentSession()
	if sess != nil && (sess.ID == identifier || sess.Name == identifier || sess.Description == identifier) {
		h.addScrollback("cannot delete the current session - switch sessions first")
		return
	}

	if err := h.tui.sessionMgr.DeleteSession(context.TODO(), identifier); err != nil {
		h.addError(err)
		return
	}
	h.addScrollback(fmt.Sprintf("deleted session: %s", identifier))
}

func (h *CommandHandler) handleTasks() {
	h.addSectionHeader("tasks")
	resp, err := h.tui.client.ListTasksExtended()
	if err != nil {
		h.addError(err)
		return
	}
	if len(resp.Tasks) == 0 {
		h.addScrollback("  (no tasks)")
		return
	}
	for i, t := range resp.Tasks {
		displayName := t.Name
		if t.Description != "" && len(t.Description) > 40 {
			displayName = t.Description[:37] + "..."
		}
		h.addScrollback(fmt.Sprintf("  [%d] %s  state=%s  completed=%d/%d",
			i, displayName, t.State, t.CompletedJobs, t.TotalJobs))
		if len(t.ChildTasks) > 0 {
			h.addScrollback(fmt.Sprintf("       child tasks: %d", len(t.ChildTasks)))
		}
		if t.TokenUsage > 0 {
			h.addScrollback(fmt.Sprintf("       tokens: %d", t.TokenUsage))
		}
	}
}

func (h *CommandHandler) handleCancel(_ context.Context, cmd *sharedclient.SlashCommand) {
	if len(cmd.Args) == 0 {
		h.addScrollback("usage: /cancel <task-id>")
		return
	}
	taskID := cmd.Args[0]
	if err := h.tui.client.CancelTask(taskID); err != nil {
		h.addError(err)
		return
	}
	h.addScrollback(fmt.Sprintf("task %s cancelled", taskID))
}

func (h *CommandHandler) handleAmend(_ context.Context, cmd *sharedclient.SlashCommand) {
	if len(cmd.Args) < 2 {
		h.addScrollback("usage: /amend <task-id> <field> [value]")
		h.addScrollback("  fields: name, description, state")
		return
	}
	taskID := cmd.Args[0]
	field := cmd.Args[1]
	value := ""
	if len(cmd.Args) > 2 {
		value = strings.Join(cmd.Args[2:], " ")
	}

	// Read current task for context
	task, err := h.tui.client.GetTask(taskID)
	if err != nil {
		h.addError(err)
		return
	}

	switch field {
	case "name", "description", "state":
		h.addScrollback(fmt.Sprintf("amending task %s: %s = ", taskID, field))
		if value != "" {
			h.addScrollback(value)
		} else {
			h.addScrollback(fmt.Sprintf("(current value: %s)", task.Description))
		}
	default:
		h.addScrollback(fmt.Sprintf("unknown amend field: %s (try: name, description, state)", field))
		return
	}
}

func (h *CommandHandler) handleInterrupt(_ context.Context, cmd *sharedclient.SlashCommand) {
	if len(cmd.Args) == 0 {
		h.addScrollback("usage: /interrupt <task-id> [reason]")
		return
	}
	taskID := cmd.Args[0]
	reason := ""
	if len(cmd.Args) > 1 {
		reason = strings.Join(cmd.Args[1:], " ")
	}

	if err := h.tui.client.CancelTask(taskID); err != nil {
		h.addScrollback(fmt.Sprintf("interrupt: cancel failed: %v", err))
		return
	}
	if reason != "" {
		h.addScrollback(fmt.Sprintf("task %s interrupted: %s", taskID, reason))
	} else {
		h.addScrollback(fmt.Sprintf("task %s interrupted", taskID))
	}
}
