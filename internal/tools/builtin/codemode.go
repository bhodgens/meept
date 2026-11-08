// Package builtin: the `codemode` tool (token-efficiency branch 02 leaf 02).
//
// CodemodeTool runs model-written JavaScript in the C3 goja sandbox
// (internal/codemode) and batches calls to the agent's already-filtered tool
// registry through the sandbox's tool-bridge surface. The whole point is
// context savings: nested tool calls never enter LLM context — only the one
// C3 Result (Output + Value) does. See docs/workflows/codemode.md.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/agent"
	codemode "github.com/caimlas/meept/internal/codemode"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

const toolCodemode = "codemode"

// Timeout + memory bounds for the codemode sandbox gate.
const (
	codemodeDefaultTimeoutS = 60
	codemodeMinTimeoutS     = 1
	codemodeMaxTimeoutS     = 600
	// codemodeDefaultMemLimit bounds a single script's allocation-counting
	// budget (C3 MemoryLimit; the sandbox maps it 1:1 to the interrupt
	// threshold). 128 MiB is headroom for legitimate batch work while still
	// tripping the memory-cap interrupt well before host-OOM territory (the
	// spike measured 5M x 10-char-string pushes at ~100-200MB host heap
	// aborting inside a 64MB cap).
	codemodeDefaultMemLimit = 128 * 1024 * 1024
)

// CodemodeTool is the agent-facing `codemode` tool. It is registered on the
// shared registry only when [agent.tools].codemode_enabled is on, and
// unioned into every agent's filtered tool allow-list under the same gate
// (internal/agent filterTools). Its Execute builds the sandbox's tool
// bridges STRICTLY from the agent's already-filtered registry (injected by
// the executor via the tool-registry context key) — it never widens the
// agent's tool access, it only batches the calls the agent is already
// permitted to make.
type CodemodeTool struct {
	tools.ToolDefaults
}

// NewCodemodeTool creates a codemode tool. It needs no constructor deps: the
// tool resolves the agent's filtered registry from the context at Execute
// time (the executor injects it), so registration is gate-only.
func NewCodemodeTool() *CodemodeTool {
	return &CodemodeTool{}
}

func (t *CodemodeTool) Name() string { return toolCodemode }

func (t *CodemodeTool) Category() string { return "meta" }

// Description is model-facing and teaches the context-savings usage: nested
// tool calls do not consume context; use this for mechanical multi-call
// work.
func (t *CodemodeTool) Description() string {
	return "Run a JavaScript script that batch-calls your other tools in one shot. " +
		"The script runs in a sandbox with no filesystem or network of its own: its ONLY " +
		"capability is calling the tools you already have, via tools.<name>(args) (args is " +
		"a JSON string; enumerate the available bridges and their input schemas on " +
		"ALL_TOOLS). Context savings: nested tool calls do NOT appear in your context — " +
		"only this script's Output (accumulated via text(...)) and its return Value " +
		"(serialized as JSON) do. Use it for mechanical multi-call work — batch lookups, " +
		"shaping data across several tools, fan-out loops — instead of issuing each call " +
		"separately. Set timeout_s (seconds, default 60, clamped to [1,600]) to bound long " +
		"runs; on expiry the sandbox hard-kills the script and reports it in Result.Err."
}

// Parameters is the tool's input schema: {script (string, required),
// timeout_s (integer, default 60, max 600)}.
func (t *CodemodeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"script": {
				Type: schemaTypeString,
				Description: "The JavaScript script to run. The body may use a top-level " +
					"return; tools.<name>(argsJSON) calls one of your tools; text(...) " +
					"appends to Result.Output; ALL_TOOLS lists the available bridges.",
			},
			"timeout_s": {
				Type:        schemaTypeInteger,
				Description: "Wall-clock cap for the run in seconds. Default 60, clamped to [1,600]. On expiry the sandbox hard-kills the script and reports it in Result.Err.",
			},
		},
		Required: []string{"script"},
	}
}

// Execute builds the sandbox's tool bridges from the agent's filtered
// registry, runs the script, and returns the C3 Result serialized to JSON
// (field names OK/Output/Value/Err are frozen by the e2e gate).
func (t *CodemodeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	script, _ := args["script"].(string)
	if script == "" {
		return nil, fmt.Errorf("codemode: 'script' is required and must be a non-empty string")
	}

	timeoutS := codemodeDefaultTimeoutS
	if raw, present := args["timeout_s"]; present && raw != nil {
		fs, ok := raw.(float64) // JSON numbers decode to float64.
		if !ok {
			return nil, fmt.Errorf("codemode: 'timeout_s' must be an integer, got %T", raw)
		}
		timeoutS = int(fs)
	}
	if timeoutS < codemodeMinTimeoutS {
		timeoutS = codemodeMinTimeoutS
	}
	if timeoutS > codemodeMaxTimeoutS {
		timeoutS = codemodeMaxTimeoutS
	}

	// T5 bridge-set safety: build the sandbox's tool bridges ONLY from the
	// agent's already-filtered registry (injected by the executor via
	// ContextWithToolRegistry). Never widen access: a bridge is callable iff
	// the agent's own filtered view exposes it.
	reg := agent.ToolRegistryFromContext(ctx)
	if reg == nil {
		return nil, fmt.Errorf("codemode: no tool registry in context (the executor did not inject the filtered registry)")
	}
	bridges := codemodeBridgesFromRegistry(reg)
	if len(bridges) == 0 {
		return nil, fmt.Errorf("codemode: the agent's filtered registry exposes no tools to bridge")
	}

	sb, err := codemode.New(codemode.Options{
		Timeout:     time.Duration(timeoutS) * time.Second,
		MemoryLimit: codemodeDefaultMemLimit,
		Tools:       bridges,
	})
	if err != nil {
		return nil, fmt.Errorf("codemode: sandbox init: %w", err)
	}
	defer func() { _ = sb.Close() }() //nolint:errcheck // Close is best-effort; the result is what matters.

	res, err := sb.Execute(ctx, script)
	if err != nil {
		// Host-side failure (closed sandbox / dead loop); script-level
		// failures live in res.Err and return a nil error.
		return nil, fmt.Errorf("codemode: execution: %w", err)
	}

	// Return the C3 Result struct (not a pre-marshaled JSON string). The
	// executor marshals ExecutionResult{Result: res} into the tool envelope,
	// so the Result lands as a NESTED object whose keys (OK/Output/Value/Err
	// — the verbatim C3 field names, Result carries no JSON tags) appear
	// un-escaped. A pre-marshaled string would embed the C3 JSON as an
	// escaped string value and the e2e field-name pin (codemode-05) would
	// not see the keys. The model-facing envelope still carries the C3
	// Result JSON (object form), as frozen by the gate.
	return res, nil
}

// codemodeBridgesFromRegistry maps the agent's filtered tool registry onto
// the C3 ToolBridge surface. The set equals the filtered registry's tools
// exactly (T5: no additions, no omissions) — shell/file_write appear only
// if the agent's own filtered registry has them. Each Call closure re-enters
// the SAME filtered registry by name, so a nested call can never escape the
// agent's tool scope.
func codemodeBridgesFromRegistry(reg agent.ToolRegistry) []codemode.ToolBridge {
	list := reg.List()
	bridges := make([]codemode.ToolBridge, 0, len(list))
	for _, tool := range list {
		if tool == nil {
			continue
		}
		name := tool.Name()
		bridges = append(bridges, codemode.ToolBridge{
			Name:        name,
			Description: tool.Description(),
			InputSchema: schemaToJSONMap(tool.Parameters()),
			Call:        codemodeBridgeCall(reg, name),
		})
	}
	return bridges
}

// codemodeBridgeCall returns the ToolBridge Call for one named tool: parse
// the JSON args, invoke the tool through the filtered registry, and
// serialize the result back to a JSON string (the C3 JSON-out convention).
// An unresolvable name or execution error rejects the bridge promise with a
// message the script can catch — it does NOT widen or fabricate access.
func codemodeBridgeCall(reg agent.ToolRegistry, name string) func(context.Context, string) (string, error) {
	return func(ctx context.Context, argsJSON string) (string, error) {
		// bughunt 2026-11-07 agent-fixes F-C: the §6 row-4 hard gate lives
		// in Executor.Execute, but this bridge re-enters the filtered
		// registry directly — without the same check a sandbox script could
		// invoke any known-but-inactive tool and skip the tool_view
		// promotion round-trip entirely. The plan rides the ctx the codemode
		// tool received from the executor, so the verdict is the episode's.
		if refuse, directive := agent.ExposureGateRefusal(ctx, name); refuse {
			return "", fmt.Errorf("codemode: %s", directive)
		}
		tool := reg.Get(name)
		if tool == nil {
			return "", fmt.Errorf("tool %q is not available in this agent's scope", name)
		}
		var argsMap map[string]any
		if argsJSON != "" && argsJSON != "{}" {
			if err := json.Unmarshal([]byte(argsJSON), &argsMap); err != nil {
				return "", fmt.Errorf("%s: parse args: %w", name, err)
			}
		}
		res, err := tool.Execute(ctx, argsMap)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		out, merr := json.Marshal(res)
		if merr != nil {
			return "", fmt.Errorf("%s: serialize result: %w", name, merr)
		}
		return string(out), nil
	}
}

// schemaToJSONMap converts a tool's llm.FunctionParameters into the
// map[string]any input-schema shape the C3 ToolBridge carries (the sandbox
// exposes it on ALL_TOOLS.<name>.inputSchema for scripts to read).
func schemaToJSONMap(p llm.FunctionParameters) map[string]any {
	m := map[string]any{}
	if p.Type != "" {
		m["type"] = p.Type
	}
	if len(p.Properties) > 0 {
		props := make(map[string]any, len(p.Properties))
		for k, v := range p.Properties {
			pp := map[string]any{}
			if v.Type != "" {
				pp["type"] = v.Type
			}
			if v.Description != "" {
				pp["description"] = v.Description
			}
			props[k] = pp
		}
		m["properties"] = props
	}
	if len(p.Required) > 0 {
		req := make([]any, 0, len(p.Required))
		for _, r := range p.Required {
			req = append(req, r)
		}
		m["required"] = req
	}
	return m
}
