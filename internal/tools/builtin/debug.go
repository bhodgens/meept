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

func (t *DebugTool) Description() string {
	return "Debug a program using the Debug Adapter Protocol (DAP). " +
		"Supports launching programs, setting breakpoints, stepping through code, " +
		"inspecting variables, and evaluating expressions. Use the 'action' parameter " +
		"to specify the operation: launch, set_breakpoint, continue, step_over, step_in, " +
		"step_out, pause, evaluate, stack_trace, threads, scopes, variables, terminate, sessions."
}

func (t *DebugTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"action": {
				Type: schemaTypeString,
				Description: "The debug action to perform. " +
					"One of: launch, set_breakpoint, remove_breakpoint, continue, step_over, " +
					"step_in, step_out, pause, evaluate, stack_trace, threads, scopes, " +
					"variables, terminate, sessions.",
				Enum: []string{
					"launch", "set_breakpoint", "remove_breakpoint",
					"continue", "step_over", "step_in", "step_out", "pause",
					"evaluate", "stack_trace", "threads", "scopes", "variables",
					"terminate", "sessions",
				},
			},
			"program": {
				Type:        schemaTypeString,
				Description: "Path to the program to debug (for launch action).",
			},
			"adapter": {
				Type:        schemaTypeString,
				Description: "Debug adapter to use (for launch action). Auto-detected if empty. Options: dlv, gdb, lldb-dap, debugpy, codelldb.",
			},
			"args": {
				Type:        schemaTypeString,
				Description: "Command-line arguments for the program as a JSON array string (for launch action).",
			},
			"cwd": {
				Type:        schemaTypeString,
				Description: "Working directory for the debug session.",
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
				Description: "Thread ID (for continue, step_over, step_in, step_out, stack_trace).",
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
			"levels": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of stack frames to return (for stack_trace action).",
			},
		},
		Required: []string{"action"},
	}
}

func (t *DebugTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	if action == "" {
		return nil, fmt.Errorf("action is required")
	}

	switch action {
	case "launch":
		return t.launch(ctx, args)
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

	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"session_id": session.ID,
			"adapter":    session.Adapter,
			"state":      string(session.State),
		},
	}, nil
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

	body, err := session.Client.StackTrace(ctx, stArgs)
	if err != nil {
		return nil, err
	}

	return tools.ToolResult{Success: true, Result: rawToMap(body)}, nil
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

var errNoActiveSession = fmt.Errorf("no active debug session; use the launch action first")

// Ensure DebugTool implements tools.Tool.
var _ tools.Tool = (*DebugTool)(nil)
