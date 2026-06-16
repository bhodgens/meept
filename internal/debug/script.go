package debug

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ScriptCommand represents a single command in a debug script.
type ScriptCommand struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params,omitempty"`
	Line   int            `json:"line"` // 1-based line number in the script file
}

// ScriptResult holds the result of executing a single script command.
type ScriptResult struct {
	Index   int    `json:"index"`  // 0-based command index
	Line    int    `json:"line"`   // 1-based line number in the script file
	Action  string `json:"action"` // action name
	Success bool   `json:"success"`
	Output  any    `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ScriptOptions controls how a debug script is executed.
type ScriptOptions struct {
	// StopOnError controls whether script execution stops on the first error.
	// When false, all commands are executed and errors are collected.
	// Default: true.
	StopOnError bool `json:"stop_on_error,omitempty"`

	// FilePath is the path to the script file.
	FilePath string `json:"file_path"`
}

// ScriptSummary holds the aggregate results of executing a debug script.
type ScriptSummary struct {
	Total     int            `json:"total"`
	Succeeded int            `json:"succeeded"`
	Failed    int            `json:"failed"`
	Results   []ScriptResult `json:"results"`
}

// ParseScriptFile reads a JSON-lines script file and returns a slice of
// ScriptCommands. Each line must be a valid JSON object with at least an
// "action" field. Lines that are empty or start with "//" are skipped.
func ParseScriptFile(path string) ([]ScriptCommand, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open script file %q: %w", path, err)
	}
	defer f.Close()

	return ParseScript(f)
}

// ParseScript reads JSON-lines from an io.Reader and returns a slice of
// ScriptCommands. Each line must be a valid JSON object with at least an
// "action" field. Lines that are empty or start with "//" are skipped.
func ParseScript(r io.Reader) ([]ScriptCommand, error) {
	var commands []ScriptCommand
	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("line %d: invalid JSON: %w", lineNum, err)
		}

		action, _ := raw["action"].(string)
		if action == "" {
			return nil, fmt.Errorf("line %d: missing or empty 'action' field", lineNum)
		}

		// Extract params: everything except "action" goes into params.
		params := make(map[string]any)
		for k, v := range raw {
			if k != "action" {
				params[k] = v
			}
		}

		commands = append(commands, ScriptCommand{
			Action: action,
			Params: params,
			Line:   lineNum,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading script: %w", err)
	}

	if len(commands) == 0 {
		return nil, fmt.Errorf("script contains no commands")
	}

	return commands, nil
}

// CommandExecutor is a function that executes a single debug command given
// a context and parameter map. It returns the result and an error.
type CommandExecutor func(ctx context.Context, args map[string]any) (any, error)

// ExecuteScript runs a sequence of ScriptCommands using the provided executor.
// It collects results for each command and returns a ScriptSummary.
func ExecuteScript(ctx context.Context, commands []ScriptCommand, executor CommandExecutor, opts ScriptOptions) *ScriptSummary {
	summary := &ScriptSummary{
		Total:   len(commands),
		Results: make([]ScriptResult, 0, len(commands)),
	}

	stopOnError := opts.StopOnError // default false (zero value)

	for i, cmd := range commands {
		// Build args: merge action with params.
		args := make(map[string]any, len(cmd.Params)+1)
		args["action"] = cmd.Action
		for k, v := range cmd.Params {
			args[k] = v
		}

		result := ScriptResult{
			Index:  i,
			Line:   cmd.Line,
			Action: cmd.Action,
		}

		output, err := executor(ctx, args)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			summary.Failed++
			slog.Warn("script command failed",
				"index", i,
				"line", cmd.Line,
				"action", cmd.Action,
				"error", err,
			)
		} else {
			result.Success = true
			result.Output = output
			summary.Succeeded++
		}

		summary.Results = append(summary.Results, result)

		if stopOnError && !result.Success {
			slog.Info("script stopped on error",
				"file", opts.FilePath,
				"command_index", i,
				"action", cmd.Action,
			)
			break
		}

		// Check context cancellation.
		if ctx.Err() != nil {
			slog.Info("script cancelled",
				"file", opts.FilePath,
				"command_index", i,
			)
			break
		}
	}

	return summary
}
