package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
)

// maxCritiqueVerdicts is the verdict bound from the leaf: only the LAST
// three prior review verdicts ride into the critique input, no matter how
// many reviewed steps the session has accumulated.
const maxCritiqueVerdicts = 3

// failureBlockMaxChars is the ceiling on the rendered failure block.
// failureBlockMaxChars already caps buildFailureBlock output (2af298b1);
// this pin exists so the bound is enforced locally too — a hand-built
// FailureBlock passed through BuildPlanCritiqueInput cannot regrow the
// critic prompt past the same limit.
const critiqueFailureBlockMaxChars = failureBlockMaxChars

// critiqueObjection is a synthetic blocking objection: a problem with the
// draft that is decided at assembly time, without spending a critic call.
type critiqueObjection struct {
	// Phase is the draft phase the objection applies to ("" for
	// plan-level objections).
	Phase string
	// Hint is the unknown tool hint that triggered the objection.
	Hint string
	// Reason is the human/LLM-readable blocking reason.
	Reason string
}

// String renders the objection as a single prompt line.
func (o critiqueObjection) String() string {
	if o.Phase != "" {
		return fmt.Sprintf("BLOCKING (tool coverage): phase %q uses unknown tool %q: %s", o.Phase, o.Hint, o.Reason)
	}
	return fmt.Sprintf("BLOCKING (tool coverage): unknown tool %q: %s", o.Hint, o.Reason)
}

// PlanCritiqueInput is the evidence base the plan critic consumes
// (tiered-iteration leaf 03). The planner never gathers this itself:
// the CALLER composes evidence, the critic judges, the planner writes.
//
// Every field is optional. Rendering (Render) omits empty/nil sections
// entirely so the critic prompt never contains "null" or empty-list noise.
type PlanCritiqueInput struct {
	// PriorReviewVerdicts carries the last (bounded) review verdicts from
	// this session — evidence of what similar work already failed or
	// passed review. Source: StepStore.ReviewVerdictsForSession.
	PriorReviewVerdicts []task.ReviewVerdictSummary
	// FailureBlock is the "## Previous attempt failed" block from the
	// failure-triggered replan path (buildFailureBlock, 2af298b1),
	// reused verbatim — no second format.
	FailureBlock string
	// ValidTools is the registry's tool-name set (ToolRegistry.Names(),
	// the same source the planner's SetValidToolNames wiring uses), so
	// critic and parse-time tool-hint checks can never disagree about
	// what a valid tool is.
	ValidTools map[string]bool
	// PriorDraft is the previous PlanDraft, present from critique round
	// 2 onward (nil on the first critique).
	PriorDraft *PlanDraft
}

// CritiqueInputSources bundles the live system handles the assembler reads.
// All fields are optional; every nil/empty source degrades to an omitted
// section, never an error (nil-safe per the leaf's pins).
type CritiqueInputSources struct {
	// StepStore, when non-nil, supplies the session's prior review
	// verdicts (bounded read, last maxCritiqueVerdicts).
	StepStore *task.StepStore
	// Registry, when non-nil, supplies ValidTools from Names() — the
	// same source as the planner's tool-hint validation wiring
	// (internal/daemon/components.go SetValidToolNames site). Declared
	// as the concrete *tools.Registry because the in-package
	// agent.ToolRegistry interface (executor.go) predates Names() and
	// is satisfied by the same production registry.
	Registry *tools.Registry
	// SessionID scopes the verdict read; cross-session retrieval is out
	// of scope (the memory system's job, later).
	SessionID string
	// Failures, when non-empty, renders the replan failure block via
	// buildFailureBlock (2af298b1) — no second format.
	Failures []stepFailure
	// PriorDraft, when non-nil, is carried through for rounds ≥ 2.
	PriorDraft *PlanDraft
}

// BuildPlanCritiqueInput assembles the critic's evidence base from the
// live sources. Nil-safe: a nil step store yields zero verdicts (not an
// error), a nil registry yields an empty ValidTools set (coverage checks
// then skip rather than flag everything unknown), and no failures yields
// an empty FailureBlock. The returned input's FailureBlock is always
// bounded (≤ failureBlockMaxChars).
func BuildPlanCritiqueInput(sources CritiqueInputSources) (*PlanCritiqueInput, error) {
	input := &PlanCritiqueInput{}

	// Prior review verdicts (session-scoped, bounded last-3 read).
	if sources.StepStore != nil {
		verdicts, err := sources.StepStore.ReviewVerdictsForSession(sources.SessionID, maxCritiqueVerdicts)
		if err != nil {
			return nil, fmt.Errorf("failed to load prior review verdicts: %w", err)
		}
		input.PriorReviewVerdicts = verdicts
	}

	// Registry tool names — the same set the planner's tool-hint
	// validation is wired with, so critic and parse-time checks agree.
	if sources.Registry != nil {
		names := sources.Registry.Names()
		if len(names) > 0 {
			input.ValidTools = make(map[string]bool, len(names))
			for _, n := range names {
				input.ValidTools[n] = true
			}
		}
	}

	// Replan failure evidence — the 2af298b1 block, reused directly.
	input.FailureBlock = boundedFailureBlock(buildFailureBlock(sources.Failures))

	// Prior draft (rounds ≥ 2).
	input.PriorDraft = sources.PriorDraft

	return input, nil
}

// boundedFailureBlock clamps a failure block to critiqueFailureBlockMaxChars
// runes so even a caller-supplied block cannot regrow the critic prompt.
func boundedFailureBlock(block string) string {
	if len([]rune(block)) <= critiqueFailureBlockMaxChars {
		return block
	}
	return truncateRunes(block, critiqueFailureBlockMaxChars-1, "…")
}

// ValidateToolHints is the tool-coverage pre-check: it returns a synthetic
// blocking objection for every draft tool hint that is NOT in ValidTools
// (the leaf: such objections are decided at assembly time — no critic call
// is spent on that class). Hints are checked case-insensitively so a
// "Shell" hint matches a registered "shell" tool.
//
// The planner's parse-time validation (SetValidToolNames) drops unknown
// hints; this check is the critic-side twin and deliberately blocks instead
// of silently dropping, so the draft author sees the problem.
//
// Conversational hints ("chat", "report", "plan", …) are not registry tool
// names and never were — they name agent roles, not tools — so they are
// exempt here exactly as reviewHintIsConversational exempts them elsewhere.
// Empty ValidTools (nil registry) disables the check: absence of registry
// information must not block every draft.
func (in *PlanCritiqueInput) ValidateToolHints(hints []string) []critiqueObjection {
	if in == nil || len(in.ValidTools) == 0 || len(hints) == 0 {
		return nil
	}
	var objections []critiqueObjection
	for _, hint := range hints {
		lower := strings.ToLower(strings.TrimSpace(hint))
		if lower == "" || reviewHintIsConversational(lower) {
			continue
		}
		if in.ValidTools[lower] {
			continue
		}
		objections = append(objections, critiqueObjection{
			Hint:   lower,
			Reason: fmt.Sprintf("tool %q is not in the registry; replace the hint with a registered tool or drop it so the executor's tool-hint table picks", lower),
		})
	}
	return objections
}

// Render renders the input as critic-prompt sections. Empty/nil sources
// omit their sections entirely: the output never contains "null", empty
// lists, or placeholder noise. The returned string is "" when no section
// has content (callers then skip attaching an evidence block at all).
func (in *PlanCritiqueInput) Render() string {
	if in == nil {
		return ""
	}
	var sb strings.Builder

	if len(in.PriorReviewVerdicts) > 0 {
		sb.WriteString("## Prior review verdicts (this session)\n")
		sb.WriteString("What similar steps' reviews decided recently:\n")
		for _, v := range in.PriorReviewVerdicts {
			fmt.Fprintf(&sb, "- [%s] %s", v.Verdict, v.Description)
			if v.Reason != "" {
				fmt.Fprintf(&sb, " — %s", v.Reason)
			}
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	if in.FailureBlock != "" {
		sb.WriteString(strings.TrimRight(in.FailureBlock, "\n"))
		sb.WriteString("\n\n")
	}

	if len(in.ValidTools) > 0 {
		names := make([]string, 0, len(in.ValidTools))
		for n := range in.ValidTools {
			names = append(names, n)
		}
		sort.Strings(names)
		sb.WriteString("## Valid tools (step tool hints must name one of these)\n")
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n\n")
	}

	if in.PriorDraft != nil && strings.TrimSpace(in.PriorDraft.Markdown) != "" {
		sb.WriteString("## Prior draft (critique round 2+)\n")
		md := in.PriorDraft.Markdown
		const maxPriorDraftChars = 2000
		if len(md) > maxPriorDraftChars {
			md = truncateRunes(md, maxPriorDraftChars-1, "…")
		}
		sb.WriteString(md)
		sb.WriteString("\n\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}
