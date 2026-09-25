package agent

import (
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// Tool-call exemplar injection (2026-09-24 8B prose-answer investigation).
//
// Live probes of the LFM2.5-8B GGUF on the T1-shaped prompt ("create a file
// named hello.txt ...") found the model declining to act with "the
// functionality to create a file is not available in the provided tools" —
// the base model does not believe a write-shaped tool can create, and it was
// never trained to emit tool calls reliably (tool_choice=required was
// honored only 2/5 by the server/model pair). A worked example is proof by
// demonstration: showing a COMPLETED call succeeded where prose instruction
// failed.
//
// Gating is deliberately narrow, per operator decision:
//   - executor role only (same gate as tool_choice: "required" — a worked
//     example on conversational turns invites spurious calls);
//   - LFM-family models only (model id or provider id containing "lfm",
//     case-insensitive). Tool-tuned models treat the example as redundant
//     context; until measured, we do not pay its cost elsewhere.
//
// Parrot guard: the example uses OBVIOUS placeholder values, so a weak model
// copying literal values produces visibly-wrong output rather than
// plausibly-wrong output.

// maxExemplarTools bounds how many real tool names are woven into the
// example. Registry order is stabilized upstream; the first few names are
// enough for demonstration.
const maxExemplarTools = 3

// isLFMFamilyModel reports whether the serving model/provider belongs to the
// LFM family — the only family the exemplar section is enabled for (operator
// decision 2026-09-24). Empty ids = unknown = disabled.
func isLFMFamilyModel(modelID, providerID string) bool {
	return containsFold(modelID, "lfm") || containsFold(providerID, "lfm")
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// buildToolExemplarSectionForCurrentModel resolves the loop's current model
// identity and stabilizes the tool definition list, then delegates to
// buildToolExemplarSection. Returns "" when any gate (executor role, LFM
// family, tools present) fails.
func (l *AgentLoop) buildToolExemplarSectionForCurrentModel() string {
	if l.spec == nil || l.spec.Role != RoleExecutor || l.registry == nil {
		return ""
	}
	modelID, providerID := l.currentModelInfo()
	if !isLFMFamilyModel(modelID, providerID) {
		return ""
	}
	tools := l.registry.GetDefinitions()
	if len(tools) == 0 {
		return ""
	}
	return buildToolExemplarSection(l.spec, modelID, providerID, tools)
}

// prefillToolCallHint builds the partial tool call the retried request will
// continue: '{"name": "<first-write-capable-tool>", "arguments": {' — chosen
// from the registry's write/filesystem tools when present (the observed
// failure is always a file side-effect claim), else the first tool. Returns
// "" when the registry has no tools at all.
func (l *AgentLoop) prefillToolCallHint() string {
	if l.registry == nil {
		return ""
	}
	defs := l.registry.GetDefinitions()
	if len(defs) == 0 {
		return ""
	}
	pick := ""
	for _, d := range defs {
		switch d.Function.Name {
		case "file_write", "file_edit", "shell_execute", "run_command":
			pick = d.Function.Name
		}
		if pick != "" {
			break
		}
	}
	if pick == "" {
		pick = defs[0].Function.Name
	}
	return fmt.Sprintf(`{"name": %q, "arguments": {`, pick)
}

// buildToolExemplarSection renders the worked tool-call example for the
// system prompt. Returns "" when the section must not appear (non-executor,
// non-LFM model, or no tools). tools is the same stabilized definition list
// the request sends, so the example can never name a tool the model was not
// given.
func buildToolExemplarSection(spec *AgentSpec, modelID, providerID string, tools []llm.ToolDefinition) string {
	if spec == nil || spec.Role != RoleExecutor {
		return ""
	}
	if !isLFMFamilyModel(modelID, providerID) {
		return ""
	}
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## How to act with tools\n\n")
	sb.WriteString("You act by emitting tool calls. A tool call is not prose — it is a structured request the platform executes. Example shape (values are placeholders, use the real task's values):\n\n")
	sb.WriteString("{\"name\": \"<tool-name>\", \"arguments\": {\"<param>\": \"<value>\"}}\n\n")

	// Weave up to 3 REAL tool names from the request's own definition list,
	// so the example is always consistent with what the model was given.
	names := make([]string, 0, maxExemplarTools)
	for _, t := range tools {
		if t.Function.Name == "" {
			continue
		}
		names = append(names, t.Function.Name)
		if len(names) == maxExemplarTools {
			break
		}
	}
	if len(names) > 0 {
		fmt.Fprintf(&sb, "Tools available this session include: %s. Prefer them over describing what you would do.\n", strings.Join(names, ", "))
	}
	sb.WriteString("Do not claim an action is impossible without trying its tool first. Do not describe work you could do — call the tool and let the result become your answer.\n")
	return sb.String()
}
