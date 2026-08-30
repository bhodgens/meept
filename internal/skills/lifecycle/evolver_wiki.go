package lifecycle

import (
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/selfimprove"
)

// TraceProvider decouples the evolver from the concrete trace store (tests
// stub it). Satisfied structurally by *selfimprove.TraceStore; defined locally
// so the lifecycle package does not hard-import selfimprove for the interface
// (the WikiStore option below does import selfimprove for the concrete type).
type TraceProvider interface {
	Sample(maxFails, maxPasses, maxChars int) ([]selfimprove.TraceRecord, error)
}

// Pass A context budgets (arXiv:2608.27454 Appendix C sampling budgets). These
// live in code rather than config per master Contract 3; if they ever need to
// be tunable that is a config leaf, not this one.
const (
	traceSampleMaxFails  = 5
	traceSampleMaxPass   = 3
	traceSampleMaxChars  = 15000
	impactLedgerMaxChars = 20000

	// impactDiffMaxChars caps the per-row diff payload the evolver writes to
	// skill-impact.md so the ledger stays readable at 4k chars per verdict.
	impactDiffMaxChars = 4000
)

// WithTraceProvider injects the execution-trace source consumed by Pass A
// refine prompts. Nil is a no-op (graceful degradation).
func WithTraceProvider(tp TraceProvider) EvolverOption {
	return func(e *Evolver) {
		if tp != nil {
			e.traceProvider = tp
		}
	}
}

// WithWikiStore injects the wiki store consumed by Pass A refine prompts and
// the skill-impact ledger. Nil is a no-op (graceful degradation).
func WithWikiStore(ws *selfimprove.WikiStore) EvolverOption {
	return func(e *Evolver) {
		if ws != nil {
			e.wiki = ws
		}
	}
}

// Prompt section headers for the Pass A wiki/trace context. Frozen order:
// ledger → index → traces, prepended ahead of the per-skill usage stats.
const (
	impactLedgerHeader = "--- Prior Skill Impact (do not repeat rejected proposals) ---"
	wikiIndexHeader    = "--- Wiki Index ---"
	traceCtxHeader     = "--- Execution Traces ---"
)

// cycleWikiContext is the wiki/trace context built once per RunCycle and
// shared by every Pass A refine prompt in that cycle. All fields are optional:
// empty strings mean the corresponding source is unavailable and the section
// is omitted from the prompt.
type cycleWikiContext struct {
	ledger string // skill-impact.md content, newest rows kept when over cap
	index  string // wiki index.md content
	traces string // rendered execution traces, failures first
}

// has reports whether any context section is populated.
func (c *cycleWikiContext) has() bool {
	return c.ledger != "" || c.index != "" || c.traces != ""
}

// load populates the context from the evolver's wiki store and trace provider.
// It never fails the cycle: per-source errors are logged and degrade to an
// empty section. When both sources are nil the context stays empty, and the
// Pass A prompt is byte-identical to the pre-wiki behavior.
func (e *Evolver) loadCycleWikiContext() *cycleWikiContext {
	c := &cycleWikiContext{}

	if e.wiki != nil {
		raw, err := e.wiki.ReadSkillImpact()
		if err != nil {
			e.logger.Warn("pass A: read skill-impact ledger failed", "error", err)
		} else if raw != "" {
			c.ledger = keepNewestRows(raw, impactLedgerMaxChars)
		}
		idx, err := e.wiki.ReadIndex()
		if err != nil {
			e.logger.Warn("pass A: read wiki index failed", "error", err)
		} else {
			c.index = idx
		}
	}

	if e.traceProvider != nil {
		recs, err := e.traceProvider.Sample(traceSampleMaxFails, traceSampleMaxPass, traceSampleMaxChars)
		if err != nil {
			e.logger.Warn("pass A: sample execution traces failed", "error", err)
		} else {
			c.traces = renderTraces(recs, traceSampleMaxChars)
		}
	}

	return c
}

// keepNewestRows truncates append-only ledger content to at most maxChars,
// keeping the newest rows (the newest rejections matter most) and dropping
// whole oldest lines, never splitting a row mid-line.
func keepNewestRows(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	lines := strings.SplitAfter(content, "\n")
	// SplitAfter leaves a possibly empty final element when the content ends
	// in a newline; walk backwards accumulating until the budget is hit.
	kept := make([]string, 0, len(lines))
	total := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		if total+len(line) > maxChars {
			break
		}
		total += len(line)
		kept = append(kept, line)
	}
	// Reverse back to chronological order.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, "")
}

// renderTraces renders sampled trace records as prompt text, failures first
// (matching the sample order), one block per record:
//
//	[outcome=failure] <id> <domain>
//	<rendered steps>
//
// Each record's rendered block is capped at maxChars. Steps render as
// "- <action> (ok|fail): input -> output" so failures are self-describing.
func renderTraces(recs []selfimprove.TraceRecord, maxChars int) string {
	var b strings.Builder
	for _, rec := range recs {
		start := b.Len()
		fmt.Fprintf(&b, "[outcome=%s] %s %s\n", rec.Outcome, rec.ID, rec.Domain)
		if rec.Error != "" {
			fmt.Fprintf(&b, "  error: %s\n", rec.Error)
		}
		for _, step := range rec.Steps {
			status := "ok"
			if !step.Success {
				status = "fail"
			}
			fmt.Fprintf(&b, "  - %s (%s)", step.Action, status)
			if step.Input != "" {
				fmt.Fprintf(&b, ": %s", step.Input)
			}
			if step.Output != "" {
				fmt.Fprintf(&b, " -> %s", step.Output)
			}
			b.WriteString("\n")
		}
		if rec.Summary != "" {
			fmt.Fprintf(&b, "  summary: %s\n", rec.Summary)
		}
		if b.Len()-start > maxChars {
			// Over-budget record: hard-cut to the cap. Individual step strings
			// were already capped by the TraceProvider's maxChars; this guards
			// the per-record total.
			truncated := b.String()[:start+maxChars]
			b.Reset()
			b.WriteString(truncated)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// render produces the prompt context block to prepend ahead of the per-skill
// stats. Empty source sections are skipped entirely; an all-empty context
// yields an empty string so the prompt stays byte-identical to the pre-wiki
// behavior.
func (c *cycleWikiContext) render() string {
	if !c.has() {
		return ""
	}
	var b strings.Builder
	if c.ledger != "" {
		b.WriteString(impactLedgerHeader + "\n")
		b.WriteString(c.ledger)
		if !strings.HasSuffix(c.ledger, "\n") {
			b.WriteString("\n")
		}
	}
	if c.index != "" {
		b.WriteString(wikiIndexHeader + "\n")
		b.WriteString(c.index)
		if !strings.HasSuffix(c.index, "\n") {
			b.WriteString("\n")
		}
	}
	if c.traces != "" {
		b.WriteString(traceCtxHeader + "\n")
		b.WriteString(c.traces)
		if !strings.HasSuffix(c.traces, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// appendSkillImpact records one verifier verdict (accept or reject) as a JSONL
// row in the skill-impact ledger. Wiki nil ⇒ no-op. The diff payload is the
// candidate content truncated to impactDiffMaxChars per row.
func (e *Evolver) appendSkillImpact(verifyAction string, proposal EvolutionProposal, vr *VerificationResult) {
	if e.wiki == nil {
		return
	}
	diff := proposal.CandidateContent
	if len(diff) > impactDiffMaxChars {
		diff = diff[:impactDiffMaxChars]
	}
	entry := selfimprove.SkillImpactEntry{
		Time:      nowUTC(),
		Action:    verifyAction,
		SkillName: proposal.SkillName,
		Diff:      diff,
		Score:     vr.Score,
		Accepted:  vr.Action == ActionAccept,
		Reason:    strings.Join(vr.Reasons, "; "),
	}
	if err := e.wiki.AppendSkillImpact(entry); err != nil {
		// The ledger is advisory evidence, never a gate: a write failure must
		// not fail the proposal pipeline. Warn and move on.
		e.logger.Warn("failed to append skill-impact ledger row",
			"skill", proposal.SkillName, "error", err)
	}
}

// nowUTC returns the current UTC time; a tiny indirection so the ledger
// timestamp stays swappable in tests without exporting a mutable clock.
func nowUTC() time.Time {
	return time.Now().UTC()
}
