package tui

import (
	"encoding/json"
	"fmt"
	"strings"
)

// handleDispatchCommand implements the /dispatch slash command.
//
// usage:
//
//	/dispatch <node> <agent> <task>     submit a task to a remote node
//	/dispatch status <jobID>            query job status
//	/dispatch results <jobID>           fetch job results
//
// Mirrors the `meept dispatch` CLI subcommands for TUI/CLI parity.
func (h *CommandHandler) handleDispatchCommand(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{
			Output:  "dispatch requires daemon connection",
			IsError: true,
		}
	}

	if len(args) == 0 {
		return &CommandResult{
			Output:  "usage: /dispatch <node> <agent> <task>\n       /dispatch status <jobID>\n       /dispatch results <jobID>",
			IsError: true,
		}
	}

	sub := strings.ToLower(strings.TrimSpace(args[0]))

	switch sub {
	case "status":
		return h.dispatchStatus(args[1:])
	case "results":
		return h.dispatchResults(args[1:])
	default:
		// Not a subcommand keyword — treat the entire args list as
		// <node> <agent> <task description...>.
		return h.dispatchSubmit(args)
	}
}

// dispatchSubmit sends a dispatch.submit RPC.
func (h *CommandHandler) dispatchSubmit(args []string) *CommandResult {
	if len(args) < 3 {
		return &CommandResult{
			Output:  "usage: /dispatch <node> <agent> <task description>",
			IsError: true,
		}
	}

	params := map[string]any{
		"target_node":      args[0],
		"agent_id":         args[1],
		"task_description": strings.Join(args[2:], " "),
	}

	raw, err := h.rpc.Call("dispatch.submit", params)
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("dispatch failed: %v", err),
			IsError: true,
		}
	}

	var ack struct {
		JobID    string `json:"job_id"`
		Accepted bool   `json:"accepted"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to parse response: %v", err),
			IsError: true,
		}
	}

	if ack.Accepted {
		return &CommandResult{
			Output: fmt.Sprintf("job submitted\n  job id: %s", ack.JobID),
		}
	}
	msg := ack.Message
	if msg == "" {
		msg = "(no reason given)"
	}
	return &CommandResult{
		Output:  fmt.Sprintf("job rejected: %s", msg),
		IsError: true,
	}
}

// dispatchStatus sends a dispatch.status RPC.
func (h *CommandHandler) dispatchStatus(args []string) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Output:  "usage: /dispatch status <jobID>",
			IsError: true,
		}
	}
	jobID := strings.TrimSpace(args[0])

	raw, err := h.rpc.Call("dispatch.status", map[string]any{
		"job_id": jobID,
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("status query failed: %v", err),
			IsError: true,
		}
	}

	var status struct {
		JobID     string `json:"job_id"`
		State     string `json:"state"`
		StartedAt int64  `json:"started_at"`
		UpdatedAt int64  `json:"updated_at"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to parse response: %v", err),
			IsError: true,
		}
	}

	out := fmt.Sprintf("status\n  job id: %s\n  state:  %s", status.JobID, status.State)
	if status.Error != "" {
		out += fmt.Sprintf("\n  error:  %s", status.Error)
	}
	return &CommandResult{Output: out}
}

// dispatchResults sends a dispatch.results RPC.
func (h *CommandHandler) dispatchResults(args []string) *CommandResult {
	if len(args) < 1 {
		return &CommandResult{
			Output:  "usage: /dispatch results <jobID>",
			IsError: true,
		}
	}
	jobID := strings.TrimSpace(args[0])

	raw, err := h.rpc.Call("dispatch.results", map[string]any{
		"job_id": jobID,
	})
	if err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("results query failed: %v", err),
			IsError: true,
		}
	}

	var results []struct {
		JobID     string `json:"job_id"`
		OutputRef string `json:"output_ref"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		return &CommandResult{
			Output:  fmt.Sprintf("failed to parse response: %v", err),
			IsError: true,
		}
	}

	if len(results) == 0 {
		return &CommandResult{Output: "(no results yet)"}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "results (%d)\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&sb, "  [%d] job id: %s\n", i, r.JobID)
		if r.OutputRef != "" {
			fmt.Fprintf(&sb, "      output:  %s\n", r.OutputRef)
		}
		if r.Error != "" {
			fmt.Fprintf(&sb, "      error:   %s\n", r.Error)
		}
	}
	return &CommandResult{Output: sb.String()}
}
