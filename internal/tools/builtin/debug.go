package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/debug"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// DebugTool provides DAP debugging capabilities as a single tool with
// action-based dispatch.
type DebugTool struct {
	manager *debug.Manager
	checker *security.PermissionChecker
}

// NewDebugTool creates a new debug tool.
func NewDebugTool(manager *debug.Manager, checker *security.PermissionChecker) *DebugTool {
	return &DebugTool{
		manager: manager,
		checker: checker,
	}
}

func (t *DebugTool) Name() string { return "debug" }

func (t *DebugTool) Category() string { return "debug" }

func (t *DebugTool) Description() string {
	return "Debug a program using the Debug Adapter Protocol (DAP). " +
		"Supports launching programs, attaching to running processes, loading core dumps for " +
		"post-mortem analysis, setting breakpoints, stepping through code, inspecting " +
		"variables, and evaluating expressions. Go-specific features: goroutine listing, " +
		"goroutine switching, and goroutine-aware stack traces. Core dump analysis: " +
		"automatically detects gdb, lldb, or delve adapter based on binary and platform, " +
		"extracts stack traces, local variables, signal info, and generates a crash report. " +
		"Use the 'action' parameter to specify the operation: launch, attach, load_core, " +
		"set_breakpoint, remove_breakpoint, continue, step_over, step_in, step_out, pause, " +
		"evaluate, stack_trace, threads, scopes, variables, goroutines, set_goroutine, " +
		"script, terminate, sessions."
}

func (t *DebugTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"action": {
				Type: schemaTypeString,
				Description: "The debug action to perform. " +
					"One of: launch, attach, load_core, set_breakpoint, remove_breakpoint, " +
					"continue, step_over, step_in, step_out, pause, evaluate, stack_trace, " +
					"threads, scopes, variables, goroutines, set_goroutine, script, terminate, sessions.",
				Enum: []string{
					"launch", "attach", "load_core",
					"set_breakpoint", "remove_breakpoint",
					"continue", "step_over", "step_in", "step_out", "pause",
					"evaluate", "stack_trace", "threads", "scopes", "variables",
					"goroutines", "set_goroutine", "script",
					"terminate", "sessions",
				},
			},
			"program": {
				Type:        schemaTypeString,
				Description: "Path to the program to debug (for launch and load_core actions).",
			},
			"core_file": {
				Type:        schemaTypeString,
				Description: "Path to a core dump file for post-mortem analysis (for load_core action).",
			},
			"adapter": {
				Type:        schemaTypeString,
				Description: "Debug adapter to use (for launch and load_core actions). Auto-detected if empty. Options: dlv, gdb, lldb-dap, debugpy, codelldb. For load_core: gdb, lldb, delve.",
			},
			"args": {
				Type:        schemaTypeString,
				Description: "Command-line arguments for the program as a JSON array string (for launch action).",
			},
			"cwd": {
				Type:        schemaTypeString,
				Description: "Working directory for the debug session.",
			},
			"process_id": {
				Type:        schemaTypeInteger,
				Description: "Process ID to attach to (for attach action).",
			},
			"process_name": {
				Type:        schemaTypeString,
				Description: "Process name to look up and attach to (for attach action). Resolved to a PID via system process list.",
			},
			"file": {
				Type:        schemaTypeString,
				Description: "Source file path (for set_breakpoint, remove_breakpoint).",
			},
			"line": {
				Type:        schemaTypeInteger,
				Description: "Line number (for set_breakpoint, remove_breakpoint).",
			},
			"condition": {
				Type:        schemaTypeString,
				Description: "Breakpoint condition expression (optional, for set_breakpoint).",
			},
			"thread_id": {
				Type:        schemaTypeInteger,
				Description: "Thread ID (for continue, step_over, step_in, step_out, stack_trace). For Go sessions, this may map to a goroutine.",
			},
			"frame_id": {
				Type:        schemaTypeInteger,
				Description: "Stack frame ID (for scopes, evaluate).",
			},
			"expression": {
				Type:        schemaTypeString,
				Description: "Expression to evaluate (for evaluate action).",
			},
			"context": {
				Type:        schemaTypeString,
				Description: "Evaluation context: 'watch', 'repl', or 'hover' (for evaluate action).",
			},
			"variable_ref": {
				Type:        schemaTypeInteger,
				Description: "Variables reference ID (for variables action).",
			},
			"goroutine_id": {
				Type:        schemaTypeInteger,
				Description: "Goroutine ID (for goroutines list filter, set_goroutine, and optional goroutine-aware stack_trace).",
			},
			"levels": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of stack frames to return (for stack_trace action).",
			},
			"script_file": {
				Type:        schemaTypeString,
				Description: "Path to a JSON-lines script file containing debug commands to execute sequentially (for script action). Each line is a JSON object with an 'action' field and optional parameters.",
			},
			"stop_on_error": {
				Type:        schemaTypeBoolean,
				Description: "If true, stop script execution on the first command failure. If false (default), continue executing remaining commands and collect all errors (for script action).",
			},
		},
		Required: []string{"action"},
	}
}

func (t *DebugTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("debug manager not configured")
	}

	action, _ := args["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "launch":
		return t.launch(ctx, args)
	case "attach":
		return t.attach(ctx, args)
	case "load_core":
		return t.loadCore(ctx, args)
	case "set_breakpoint":
		return t.setBreakpoint(ctx, args)
	case "remove_breakpoint":
		return t.removeBreakpoint(ctx, args)
	case "continue":
		return t.continueExecution(ctx, args)
	case "step_over":
		return t.stepOver(ctx, args)
	case "step_in":
		return t.stepIn(ctx, args)
	case "step_out":
		return t.stepOut(ctx, args)
	case "pause":
		return t.pause(ctx, args)
	case "evaluate":
		return t.evaluate(ctx, args)
	case "stack_trace":
		return t.stackTrace(ctx, args)
	case "threads":
		return t.threads(ctx, args)
	case "scopes":
		return t.scopes(ctx, args)
	case "variables":
		return t.variables(ctx, args)
	case "goroutines":
		return t.goroutines(ctx, args)
	case "set_goroutine":
		return t.setGoroutine(ctx, args)
	case "script":
		return t.script(ctx, args)
	case "terminate":
		return t.terminate(ctx, args)
	case "sessions":
		return t.sessions(ctx, args)
	default:
		return nil, fmt.Errorf("unknown debug action: %s", action)
	}
}

func (t *DebugTool) launch(ctx context.Context, args map[string]any) (any, error) {
	program, _ := args["program"].(string)
	if program == "" {
		return nil, fmt.Errorf("program path is required for launch")
	}

	// Collect args.
	var progArgs []string
	if raw, ok := args["args"]; ok {
		switch v := raw.(type) {
		case []any:
			for _, a := range v {
				if s, ok := a.(string); ok {
					progArgs = append(progArgs, s)
				}
			}
		case []string:
			progArgs = v
		}
	}

	cwd, _ := args["cwd"].(string)

	// Resolve adapter.
	var adapterCfg *debug.AdapterConfig
	if adapterName, _ := args["adapter"].(string); adapterName != "" {
		adapterCfg = debug.FindAdapterByName(adapterName)
		if adapterCfg == nil {
			return nil, fmt.Errorf("unknown adapter: %s", adapterName)
		}
	} else {
		var err error
		adapterCfg, err = debug.DetectAdapter(program, cwd)
		if err != nil {
			return nil, err
		}
	}

	session, err := t.manager.Launch(ctx, adapterCfg, program, progArgs, cwd)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"session_id": session.ID,
		"adapter":    session.Adapter,
		"state":      string(session.State),
		"mode":       string(session.Mode),
	}

	// If the program is a Go binary, suggest dlv-specific features.
	if hint := debug.GoDebugHint(program); hint != "" {
		result["hint"] = hint
	}

	return tools.ToolResult{Success: true, Result: result}, nil
}

func (t *DebugTool) attach(ctx context.Context, args map[string]any) (any, error) {
	pid := intArg(args, "process_id")
	processName, _ := args["process_name"].(string)
	if pid <= 0 && processName == "" {
		return nil, fmt.Errorf("process_id or process_name is required for attach")
	}

	// Resolve PID from process name if needed.
	if pid <= 0 && processName != "" {
		resolvedPID, err := debug.FindPIDByName(processName)
		if err != nil {
			return nil, fmt.Errorf("failed to find process %q: %w", processName, err)
		}
		pid = resolvedPID
	}

	// Resolve adapter.
	var adapterCfg *debug.AdapterConfig
	if adapterName, _ := args["adapter"].(string); adapterName != "" {
		adapterCfg = debug.FindAdapterByName(adapterName)
		if adapterCfg == nil {
			return nil, fmt.Errorf("unknown adapter: %s", adapterName)
		}
	} else {
		var err error
		adapterCfg, err = debug.DetectAdapterForProcess(pid)
		if err != nil {
			return nil, err
		}
	}

	session, err := t.manager.Attach(ctx, adapterCfg, pid, processName)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"session_id":   session.ID,
		"adapter":      session.Adapter,
		"state":        string(session.State),
		"mode":         string(session.Mode),
		"process_id":   pid,
		"process_name": processName,
	}

	// If the process binary is a Go binary, suggest dlv-specific features.
	if pid > 0 {
		if binaryPath, err := debug.FindProcessBinary(pid); err == nil {
			if hint := debug.GoDebugHint(binaryPath); hint != "" {
				result["hint"] = hint
			}
		}
	}

	return tools.ToolResult{Success: true, Result: result}, nil
}

func (t *DebugTool) loadCore(ctx context.Context, args map[string]any) (any, error) {
	coreFile, _ := args["core_file"].(string)
	program, _ := args["program"].(string)
	if coreFile == "" {
		return nil, fmt.Errorf("core_file is required for load_core")
	}
	if program == "" {
		return nil, fmt.Errorf("program path is required for load_core")
	}

	adapterName, _ := args["adapter"].(string)

	result, session, err := t.manager.LoadCore(ctx, coreFile, program, adapterName)
	if err != nil {
		return nil, err
	}

	crashReport := map[string]any{
		"session_id":  session.ID,
		"adapter":     session.Adapter,
		"state":       string(session.State),
		"mode":        string(session.Mode),
		"program":     result.Program,
		"core_file":   result.CoreFile,
		"signal":      result.Signal,
		"fault_addr":  result.FaultAddr,
		"crash_reason": result.CrashReason,
	}

	if len(result.Threads) > 0 {
		threadList := make([]map[string]any, 0, len(result.Threads))
		for _, th := range result.Threads {
			entry := map[string]any{
				"id":         th.ID,
				"is_crashed": th.IsCrashed,
			}
			if th.Reason != "" {
				entry["reason"] = th.Reason
			}
			if len(th.Stack) > 0 {
				frames := make([]map[string]any, 0, len(th.Stack))
				for _, f := range th.Stack {
					frame := map[string]any{
						"index":    f.Index,
						"function": f.Function,
					}
					if f.File != "" {
						frame["file"] = f.File
						frame["line"] = f.Line
					}
					if f.Address != "" {
						frame["address"] = f.Address
					}
					frames = append(frames, frame)
				}
				entry["stack"] = frames
			}
			threadList = append(threadList, entry)
		}
		crashReport["threads"] = threadList
	}

	if len(result.Variables) > 0 {
		vars := make([]map[string]string, 0, len(result.Variables))
		for _, v := range result.Variables {
			entry := map[string]string{
				"name":  v.Name,
				"value": v.Value,
			}
			if v.Type != "" {
				entry["type"] = v.Type
			}
			vars = append(vars, entry)
		}
		crashReport["variables"] = vars
	}

	return tools.ToolResult{Success: true, Result: crashReport}, nil
}

func (t *DebugTool) setBreakpoint(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	file, _ := args["file"].(string)
	if file == "" {
		return nil, fmt.Errorf("file is required for set_breakpoint")
	}
	line := intArg(args, "line")
	if line <= 0 {
		return nil, fmt.Errorf("line must be a positive integer")
	}

	bp := debug.SourceBreakpoint{Line: line}
	if cond, _ := args["condition"].(string); cond != "" {
		bp.Condition = cond
	}

	body, err := session.Client.SetBreakpoints(ctx, debug.SetBreakpointsArguments{
		Source:      debug.Source{Path: file},
		Breakpoints: []debug.SourceBreakpoint{bp},
	})
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
}

func (t *DebugTool) removeBreakpoint(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	file, _ := args["file"].(string)
	if file == "" {
		return nil, fmt.Errorf("file is required for remove_breakpoint")
	}

	// To remove breakpoints in DAP, we send setBreakpoints with only the
	// breakpoints we want to keep (i.e., an empty list removes all).
	body, err := session.Client.SetBreakpoints(ctx, debug.SetBreakpointsArguments{
		Source:      debug.Source{Path: file},
		Breakpoints: []debug.SourceBreakpoint{},
	})
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
}

func (t *DebugTool) continueExecution(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	threadID := intArg(args, "thread_id")
	if threadID <= 0 {
		threadID = session.CurrentThreadID
	}
	if threadID <= 0 {
		return nil, fmt.Errorf("thread_id is required (no stopped thread in session)")
	}

	if err := session.Client.Continue(ctx, threadID); err != nil {
		return nil, err
	}
	session.State = debug.SessionRunning

	return tools.ToolResult{Success: true, Result: map[string]string{"state": "running"}}, nil
}

func (t *DebugTool) stepOver(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	threadID := intArg(args, "thread_id")
	if threadID <= 0 {
		threadID = session.CurrentThreadID
	}
	if threadID <= 0 {
		return nil, fmt.Errorf("thread_id is required (no stopped thread in session)")
	}

	if err := session.Client.StepOver(ctx, threadID); err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: map[string]string{"state": "running"}}, nil
}

func (t *DebugTool) stepIn(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	threadID := intArg(args, "thread_id")
	if threadID <= 0 {
		threadID = session.CurrentThreadID
	}
	if threadID <= 0 {
		return nil, fmt.Errorf("thread_id is required (no stopped thread in session)")
	}

	if err := session.Client.StepIn(ctx, threadID); err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: map[string]string{"state": "running"}}, nil
}

func (t *DebugTool) stepOut(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	threadID := intArg(args, "thread_id")
	if threadID <= 0 {
		threadID = session.CurrentThreadID
	}
	if threadID <= 0 {
		return nil, fmt.Errorf("thread_id is required (no stopped thread in session)")
	}

	if err := session.Client.StepOut(ctx, threadID); err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: map[string]string{"state": "running"}}, nil
}

func (t *DebugTool) pause(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	threadID := intArg(args, "thread_id")
	if threadID <= 0 {
		threadID = session.CurrentThreadID
	}
	if threadID <= 0 {
		return nil, fmt.Errorf("thread_id is required")
	}

	resp, err := session.Client.SendRequest(ctx, "pause", map[string]any{"threadId": threadID})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("pause failed: %s", resp.Message)
	}

	return tools.ToolResult{Success: true, Result: map[string]string{"state": "stopped"}}, nil
}

func (t *DebugTool) evaluate(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	expression, _ := args["expression"].(string)
	if expression == "" {
		return nil, fmt.Errorf("expression is required for evaluate")
	}

	evalArgs := debug.EvaluateArguments{Expression: expression}
	if frameID := intArg(args, "frame_id"); frameID > 0 {
		evalArgs.FrameID = &frameID
	}
	if ctx2, _ := args["context"].(string); ctx2 != "" {
		evalArgs.Context = ctx2
	}

	body, err := session.Client.Evaluate(ctx, evalArgs)
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
}

func (t *DebugTool) stackTrace(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	threadID := intArg(args, "thread_id")
	if threadID <= 0 {
		threadID = session.CurrentThreadID
	}
	if threadID <= 0 {
		return nil, fmt.Errorf("thread_id is required")
	}

	stArgs := debug.StackTraceArguments{ThreadID: threadID}
	if levels := intArg(args, "levels"); levels > 0 {
		stArgs.Levels = &levels
	}

	// If goroutine_id is specified and the adapter is dlv, switch to that
	// goroutine first, then get the stack trace. The switch is transient
	// for the purpose of this call.
	goroutineID := intArg(args, "goroutine_id")
	if goroutineID > 0 && session.Adapter == "dlv" {
		if err := debug.SwitchGoroutine(ctx, session.Client, goroutineID); err != nil {
			return nil, fmt.Errorf("failed to switch to goroutine %d: %w", goroutineID, err)
		}
	}

	body, err := session.Client.StackTrace(ctx, stArgs)
	if err != nil {
		return nil, err
	}

	result := rawToMap(body)
	// Tag the result with the goroutine ID if one was used.
	if goroutineID > 0 {
		result["goroutine_id"] = goroutineID
	}

	return tools.ToolResult{Success: true, Result: result}, nil
}

func (t *DebugTool) threads(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	body, err := session.Client.Threads(ctx)
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
}

func (t *DebugTool) scopes(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	frameID := intArg(args, "frame_id")
	if frameID <= 0 {
		return nil, fmt.Errorf("frame_id is required for scopes")
	}

	body, err := session.Client.Scopes(ctx, frameID)
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
}

func (t *DebugTool) variables(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	variableRef := intArg(args, "variable_ref")
	if variableRef <= 0 {
		return nil, fmt.Errorf("variable_ref is required for variables")
	}

	varArgs := debug.VariablesArguments{VariablesReference: variableRef}
	if filter, _ := args["context"].(string); filter != "" {
		varArgs.Filter = filter
	}

	body, err := session.Client.Variables(ctx, varArgs)
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
}

func (t *DebugTool) goroutines(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	if session.Adapter != "dlv" {
		return nil, fmt.Errorf("goroutines action requires the dlv adapter (session uses %q)", session.Adapter)
	}

	result, err := debug.ListGoroutines(ctx, session.Client)
	if err != nil {
		return nil, err
	}

	// Filter by goroutine_id if specified.
	goroutineID := intArg(args, "goroutine_id")
	if goroutineID > 0 {
		filtered := make([]debug.GoroutineInfo, 0)
		for _, g := range result.List {
			if g.ID == goroutineID {
				filtered = append(filtered, g)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("goroutine %d not found among %d goroutines", goroutineID, result.Total)
		}
		return tools.ToolResult{
			Success: true,
			Result: map[string]any{
				"total":      result.Total,
				"filtered":   1,
				"goroutines": filtered,
			},
		}, nil
	}

	// Convert to serializable form.
	goroutineList := make([]map[string]any, 0, len(result.List))
	for _, g := range result.List {
		entry := map[string]any{
			"id":         g.ID,
			"status":     string(g.Status),
			"function":   g.Function,
			"file":       g.File,
			"line":       g.Line,
			"user_state": g.UserState,
		}
		if len(g.Args) > 0 {
			args := make([]map[string]string, 0, len(g.Args))
			for _, a := range g.Args {
				args = append(args, map[string]string{
					"name":  a.Name,
					"value": a.Value,
				})
			}
			entry["args"] = args
		}
		if len(g.Labels) > 0 {
			entry["labels"] = g.Labels
		}
		goroutineList = append(goroutineList, entry)
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"total":      result.Total,
			"goroutines": goroutineList,
		},
	}, nil
}

func (t *DebugTool) setGoroutine(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	if session.Adapter != "dlv" {
		return nil, fmt.Errorf("set_goroutine action requires the dlv adapter (session uses %q)", session.Adapter)
	}

	goroutineID := intArg(args, "goroutine_id")
	if goroutineID <= 0 {
		return nil, fmt.Errorf("goroutine_id is required for set_goroutine")
	}

	if err := debug.SwitchGoroutine(ctx, session.Client, goroutineID); err != nil {
		return nil, err
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"current_goroutine": goroutineID,
			"hint":              "subsequent stack_trace, scopes, and variables will operate on this goroutine",
		},
	}, nil
}

func (t *DebugTool) script(ctx context.Context, args map[string]any) (any, error) {
	scriptFile, _ := args["script_file"].(string)
	if scriptFile == "" {
		return nil, fmt.Errorf("script_file is required for script action")
	}

	// Parse the script file.
	commands, err := debug.ParseScriptFile(scriptFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse script: %w", err)
	}

	// Determine stop-on-error behavior. Default: false (continue on error).
	stopOnError := false
	if v, ok := args["stop_on_error"].(bool); ok {
		stopOnError = v
	}

	opts := debug.ScriptOptions{
		StopOnError: stopOnError,
		FilePath:    scriptFile,
	}

	// Execute the script, re-using the existing action dispatch.
	summary := debug.ExecuteScript(ctx, commands, t.Execute, opts)

	// Build a serializable result using []any for testability.
	results := make([]any, 0, len(summary.Results))
	for _, r := range summary.Results {
		entry := map[string]any{
			"index":  r.Index,
			"line":   r.Line,
			"action": r.Action,
			"success": r.Success,
		}
		if r.Output != nil {
			entry["output"] = r.Output
		}
		if r.Error != "" {
			entry["error"] = r.Error
		}
		results = append(results, entry)
	}

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"total":     summary.Total,
			"succeeded": summary.Succeeded,
			"failed":    summary.Failed,
			"results":   results,
		},
	}, nil
}

func (t *DebugTool) terminate(ctx context.Context, args map[string]any) (any, error) {
	session := t.requireActiveSession()
	if session == nil {
		return nil, errNoActiveSession
	}

	if err := t.manager.Terminate(ctx, session.ID); err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: map[string]string{"state": "terminated"}}, nil
}

func (t *DebugTool) sessions(_ context.Context, _ map[string]any) (any, error) {
	sessions := t.manager.List()
	result := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, map[string]any{
			"id":             s.ID,
			"adapter":        s.Adapter,
			"state":          string(s.State),
			"mode":           string(s.Mode),
			"program":        s.Program,
			"current_thread": s.CurrentThreadID,
			"created_at":     s.CreatedAt.Format("2006-01-02T15:04:05"),
			"last_activity":  s.LastActivity.Format("2006-01-02T15:04:05"),
		})
	}
	return tools.ToolResult{Success: true, Result: result}, nil
}

// requireActiveSession returns the active debug session or nil.
func (t *DebugTool) requireActiveSession() *debug.DebugSession {
	return t.manager.Active()
}

// intArg extracts an integer argument from the args map.
func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

// rawToMap unmarshals JSON raw bytes into a map.
func rawToMap(data json.RawMessage) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{"raw": string(data)}
	}
	return m
}

var errNoActiveSession = fmt.Errorf("no active debug session; use the launch or attach action first")

// Ensure DebugTool implements tools.Tool.
var _ tools.Tool = (*DebugTool)(nil)
