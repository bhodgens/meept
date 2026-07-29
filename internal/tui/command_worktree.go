package tui

import "fmt"

// executeWorktree handles the /worktree slash command.
//
//	/worktree           - show current worktree status
//	/worktree create    - create a worktree for the current session+project
//	/worktree remove    - release the session's worktree
func (h *CommandHandler) executeWorktree(args []string) *CommandResult {
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
	case "", "status":
		return h.executeWorktreeStatus()
	case "create":
		return h.executeWorktreeCreate()
	case "remove", "rm":
		return h.executeWorktreeRemove()
	default:
		return &CommandResult{
			Output:  fmt.Sprintf("unknown worktree subcommand: %s\nuse: /worktree [create|remove]", subcmd),
			IsError: true,
		}
	}
}

// executeWorktreeStatus shows the current session's worktree info.
func (h *CommandHandler) executeWorktreeStatus() *CommandResult {
	chat := h.getChatModel()
	if chat == nil {
		return &CommandResult{
			Output:  "no active chat session",
			IsError: true,
		}
	}

	sessionID := chat.SessionID()
	if sessionID == "" {
		return &CommandResult{
			Output:  "no session ID available",
			IsError: true,
		}
	}

	session, err := h.rpc.GetSession(sessionID)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to get session: %v", err),
			IsError: true,
		}
	}

	if session.WorktreeID == "" {
		return &CommandResult{
			Output: "no worktree bound to this session\nuse /worktree create to create one",
		}
	}

	return &CommandResult{
		Output: fmt.Sprintf("worktree id: %s\npath: %s\n", session.WorktreeID, session.WorktreePath),
	}
}

// executeWorktreeCreate creates a worktree for the current session.
func (h *CommandHandler) executeWorktreeCreate() *CommandResult {
	chat := h.getChatModel()
	if chat == nil {
		return &CommandResult{
			Output:  "no active chat session",
			IsError: true,
		}
	}

	sessionID := chat.SessionID()
	if sessionID == "" {
		return &CommandResult{
			Output:  "no session ID available",
			IsError: true,
		}
	}

	// Use the project ID from the session context if available.
	projectID := ""
	if h.getProjectID != nil {
		projectID = h.getProjectID()
	}

	result, err := h.rpc.CreateWorktree(sessionID, projectID)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to create worktree: %v", err),
			IsError: true,
		}
	}

	return &CommandResult{
		Output: fmt.Sprintf("created worktree on branch %s\npath: %s\n", result.Branch, result.Path),
	}
}

// executeWorktreeRemove releases the session's worktree.
func (h *CommandHandler) executeWorktreeRemove() *CommandResult {
	chat := h.getChatModel()
	if chat == nil {
		return &CommandResult{
			Output:  "no active chat session",
			IsError: true,
		}
	}

	sessionID := chat.SessionID()
	if sessionID == "" {
		return &CommandResult{
			Output:  "no session ID available",
			IsError: true,
		}
	}

	if err := h.rpc.RemoveWorktree(sessionID); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to remove worktree: %v", err),
			IsError: true,
		}
	}

	return &CommandResult{
		Output: "worktree removed\n",
	}
}
