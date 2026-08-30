package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (h *CommandHandler) executeEval(args []string) *CommandResult {
	if h.rpc == nil || !h.rpc.IsConnected() {
		return &CommandResult{Output: ErrNotConnected, IsError: true}
	}
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch sub {
	case "list", "":
		return h.evalList()
	case "show":
		if len(args) < 2 {
			return &CommandResult{Output: "usage: /eval show <run-id>", IsError: true}
		}
		return h.evalShow(args[1])
	default:
		return &CommandResult{Output: fmt.Sprintf("unknown eval subcommand: %s", sub), IsError: true}
	}
}

func (h *CommandHandler) evalList() *CommandResult {
	result, err := h.rpc.Call("eval.list", map[string]any{})
	if err != nil {
		return &CommandResult{Output: fmt.Sprintf("eval.list failed: %v", err), IsError: true}
	}
	runs, err := evalListPayload(result)
	if err != nil {
		return &CommandResult{Output: fmt.Sprintf("eval.list: %v", err), IsError: true}
	}
	if len(runs) == 0 {
		return &CommandResult{Output: "no eval runs"}
	}
	var sb strings.Builder
	sb.WriteString("eval runs:\n\n")
	for _, r := range runs {
		id := strMap(r, "id")
		kind := strMap(r, "kind")
		passed := boolMap(r, "passed")
		createdAt := strMap(r, "created_at")
		modelID := strMap(r, "model_id")
		taskID := strMap(r, "task_id")
		k := intMap(r, "k")
		status := "fail"
		if passed {
			status = "pass"
		}
		when := createdAt
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			when = t.Format("2006-01-02 15:04")
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s  kind=%s  model=%s  task=%s  k=%d  %s\n", status, truncate(id, 12), kind, truncate(modelID, 16), truncate(taskID, 16), k, when))
	}
	return &CommandResult{Output: sb.String()}
}

func (h *CommandHandler) evalShow(runID string) *CommandResult {
	result, err := h.rpc.Call("eval.show", map[string]any{"run_id": runID})
	if err != nil {
		return &CommandResult{Output: fmt.Sprintf("eval.show failed: %v", err), IsError: true}
	}
	var rec map[string]any
	if err := json.Unmarshal(result, &rec); err != nil {
		return &CommandResult{Output: fmt.Sprintf("parse run record: %v", err), IsError: true}
	}
	id := strMap(rec, "id")
	kind := strMap(rec, "kind")
	passed := boolMap(rec, "passed")
	createdAt := strMap(rec, "created_at")
	modelID := strMap(rec, "model_id")
	taskID := strMap(rec, "task_id")
	k := intMap(rec, "k")
	attempts := intArraySize(rec, "attempts")
	oracleName := strMap(rec, "oracle_name")
	status := "fail"
	if passed {
		status = "pass"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("eval run: %s\n", id))
	sb.WriteString(fmt.Sprintf("  status:    %s\n", status))
	sb.WriteString(fmt.Sprintf("  kind:      %s\n", kind))
	sb.WriteString(fmt.Sprintf("  model:     %s\n", modelID))
	sb.WriteString(fmt.Sprintf("  task:      %s\n", taskID))
	sb.WriteString(fmt.Sprintf("  k:         %d\n", k))
	sb.WriteString(fmt.Sprintf("  oracle:    %s\n", oracleName))
	sb.WriteString(fmt.Sprintf("  attempts:  %d\n", attempts))
	sb.WriteString(fmt.Sprintf("  created:   %s\n", createdAt))
	attemptsArr := anyArray(rec, "attempts")
	if len(attemptsArr) > 0 {
		sb.WriteString("\n  attempts:\n")
		for _, a := range attemptsArr {
			m, ok := a.(map[string]any)
			if !ok {
				continue
			}
			idx := intMap(m, "index")
			aPassed := boolMap(m, "passed")
			aStatus := "fail"
			if aPassed {
				aStatus = "pass"
			}
			sb.WriteString(fmt.Sprintf("    [%d] %s\n", idx, aStatus))
		}
	}
	return &CommandResult{Output: sb.String()}
}
