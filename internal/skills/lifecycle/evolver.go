package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/util/markdown"
)

// patternPromotionRecencyWindow is the maximum age of a learned pattern to be
// considered for promotion to a skill. Patterns older than this are considered
// stale and skipped.
const patternPromotionRecencyWindow = 14 * 24 * time.Hour // 14 days

// llmChatter is the narrow interface the evolver needs for LLM calls. Both
// *llm.Client and llm.Chatter satisfy it; we accept the interface so tests
// can inject a mock without constructing a full *llm.Client.
type llmChatter interface {
	Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error)
}

// Evolver is the closed-loop skill evolution engine. On each RunCycle it runs
// three passes:
//
//   - Pass A (refine): for each skill with enough injections, ask the LLM how
//     to improve it. Verified improvements are applied (or turned into plans).
//   - Pass B (promote): for each active learned pattern meeting confidence and
//     usage thresholds that is NOT already covered by an existing skill, create
//     a new skill. Verified creates are applied (or planned).
//   - Pass C (prune): for each low-performing skill, propose archiving it.
//     Verified archives are applied (or planned).
//
// Every proposal passes through the Verifier before being applied. When
// AutoApply is false (the default), proposals become plans via the plan manager
// instead of being applied directly.
type Evolver struct {
	usage      UsageTracker
	learning   *selfimprove.LearningPipeline
	writer     *Writer
	registry   *skills.Registry
	capIndex   *skills.CapabilityIndex
	verifier   *Verifier
	llmClient  llmChatter
	planMgr    *plan.PlanManager // nullable — when nil + AutoApply=false, proposals are just recorded
	cfg        config.SkillsEvolverConfig
	logger     *slog.Logger
}

// EvolverOption configures an Evolver at construction time.
type EvolverOption func(*Evolver)

// WithEvolverLLMChatter injects an LLM client for the evolver's refine prompts.
// This is primarily for testing; production code should pass the client via NewEvolver.
func WithEvolverLLMChatter(c llmChatter) EvolverOption {
	return func(e *Evolver) {
		if c != nil {
			e.llmClient = c
		}
	}
}

// NewEvolver constructs an Evolver from the supplied components. The verifier
// must be non-nil — every proposal passes through it. The learning pipeline,
// writer, registry, capability index, plan manager, and LLM client may be nil
// (the evolver gracefully degrades when a pass's dependencies are missing).
func NewEvolver(
	usage UsageTracker,
	learning *selfimprove.LearningPipeline,
	writer *Writer,
	registry *skills.Registry,
	capIndex *skills.CapabilityIndex,
	verifier *Verifier,
	llmClient llmChatter,
	planMgr *plan.PlanManager,
	cfg config.SkillsEvolverConfig,
	logger *slog.Logger,
	opts ...EvolverOption,
) *Evolver {
	if logger == nil {
		logger = slog.Default()
	}
	e := &Evolver{
		usage:     usage,
		learning:  learning,
		writer:    writer,
		registry:  registry,
		capIndex:  capIndex,
		verifier:  verifier,
		llmClient: llmClient,
		planMgr:   planMgr,
		cfg:       cfg,
		logger:    logger.With("component", "skill-evolver"),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

// RunCycle executes one full evolution cycle: refine, promote, prune. Returns
// an EvolutionReport with per-disposition counts and full proposal details.
// A nil report is never returned — even on total failure a report with zero
// counts is returned alongside the error.
func (e *Evolver) RunCycle(ctx context.Context) (*EvolutionReport, error) {
	report := &EvolutionReport{Details: []EvolutionProposal{}}

	// Pass A: refine existing skills.
	e.passARefine(ctx, report)

	// Pass B: promote learned patterns to skills.
	e.passBPromote(ctx, report)

	// Pass C: prune low-performing skills.
	e.passCPrune(ctx, report)

	e.logger.Info("evolution cycle complete",
		"refined", report.Refined,
		"promoted", report.Promoted,
		"pruned", report.Pruned,
		"skipped", report.Skipped,
		"rejected", report.Rejected,
		"planned", report.Planned,
	)

	return report, nil
}

// ---------------------------------------------------------------------------
// Pass A: Refine existing skills
// ---------------------------------------------------------------------------

const refineSystemPrompt = `You are a skill refinement engine for an AI agent platform. Given a skill's current content and its usage statistics, decide whether and how to improve it.

Respond in JSON format:
{
  "action": "improve_skill" | "optimize_description" | "skip",
  "rationale": "brief explanation of why this change is needed",
  "skill": "the full improved SKILL.md content (empty if action is skip)"
}`

// passARefine iterates registered skills and asks the LLM how to improve each
// one that has enough injections to be statistically meaningful.
func (e *Evolver) passARefine(ctx context.Context, report *EvolutionReport) {
	if e.usage == nil || e.registry == nil {
		e.logger.Debug("pass A (refine) skipped: usage tracker or registry not configured")
		return
	}

	allStats, err := e.usage.GetAllStats()
	if err != nil {
		e.logger.Warn("pass A: failed to get usage stats", "error", err)
		return
	}

	for _, skill := range e.registry.List() {
		stats, ok := allStats[skill.Name]
		if !ok || stats == nil {
			continue
		}
		if stats.InjectCount < e.cfg.MinInjections {
			continue
		}

		currentContent := ""
		if skill.Path != "" {
			if content, err := e.writer.ReadSkill(skill.Name); err == nil {
				currentContent = content
			}
		}

		prompt := e.buildRefinePrompt(skill.Name, currentContent, stats)
		decision, err := e.callLLMJSON(ctx, refineSystemPrompt, prompt)
		if err != nil {
			e.logger.Warn("pass A: LLM call failed for skill",
				"skill", skill.Name, "error", err)
			continue
		}

		action, _ := decision["action"].(string)
		rationale, _ := decision["rationale"].(string)
		improvedContent, _ := decision["skill"].(string)

		if action == "skip" || (action != "improve_skill" && action != "optimize_description") {
			report.Skipped++
			continue
		}

		if improvedContent == "" {
			report.Skipped++
			continue
		}

		proposal := EvolutionProposal{
			Action:           ProposalRefine,
			SkillName:        skill.Name,
			Rationale:        rationale,
			CandidateContent: improvedContent,
		}

		e.processProposal(ctx, report, proposal, currentContent, action)
	}
}

func (e *Evolver) buildRefinePrompt(name, currentContent string, stats *UsageStats) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Skill name: %s\n", name)
	fmt.Fprintf(&sb, "Usage stats: inject_count=%d, positive=%d, negative=%d, neutral=%d, effectiveness=%.2f\n",
		stats.InjectCount, stats.PositiveCount, stats.NegativeCount, stats.NeutralCount, stats.Effectiveness)
	sb.WriteString("\n--- Current Skill Content ---\n")
	sb.WriteString(currentContent)
	sb.WriteString("\n\nAnalyze the skill and its usage data. If the effectiveness is low, identify what could be improved. Return the full improved skill content or 'skip' if no change is needed.")
	return sb.String()
}

// ---------------------------------------------------------------------------
// Pass B: Promote learned patterns to skills
// ---------------------------------------------------------------------------

// passBPromote retrieves active learned patterns and promotes those meeting
// confidence and usage thresholds into new skills, provided they are not
// already covered by an existing skill.
func (e *Evolver) passBPromote(ctx context.Context, report *EvolutionReport) {
	if e.learning == nil {
		e.logger.Debug("pass B (promote) skipped: learning pipeline not configured")
		return
	}

	// Pass empty domain string so Retrieve does not filter by domain —
	// "all" is treated as a literal domain match in the Retrieve implementation
	// (internal/selfimprove/learning.go:215), which would exclude patterns from
	// specific domains (code, debugging, planning). Empty string disables the
	// domain filter entirely.
	patterns, err := e.learning.Retrieve(ctx, "", "", 100)
	if err != nil {
		e.logger.Warn("pass B: failed to retrieve patterns", "error", err)
		return
	}

	// Snapshot the set of existing skill names so we can detect collisions
	// when generating candidate names from patterns. Two patterns with similar
	// first 40 chars of description would otherwise produce the same name and
	// the second would silently overwrite the first (or fail dedup). We also
	// track names already proposed in this RunCycle so two novel patterns
	// don't collide with each other within the same pass.
	existingNames := make(map[string]bool)
	if e.registry != nil {
		for _, s := range e.registry.List() {
			existingNames[s.Name] = true
		}
	}
	proposedNames := make(map[string]bool)

	cutoff := time.Now().Add(-patternPromotionRecencyWindow)
	for _, p := range patterns {
		if p.UseCount < e.cfg.PatternPromotionUseCount {
			continue
		}
		if p.Confidence < e.cfg.PatternPromotionConfidence {
			continue
		}
		if p.Status != selfimprove.PatternStatusActive {
			continue
		}
		if p.CreatedAt.Before(cutoff) {
			continue
		}

		// Check if an existing skill already covers this pattern.
		if e.capIndex != nil {
			matches := e.capIndex.MatchWithThreshold(p.Description, 0.7, 1)
			if len(matches) > 0 {
				continue // Already covered.
			}
		}

		skillName := dedupePatternSkillName(patternToSkillName(p), existingNames, proposedNames)
		proposedNames[skillName] = true

		candidateContent := buildSkillFromPatternNamed(p, skillName)
		proposal := EvolutionProposal{
			Action:           ProposalCreate,
			SkillName:        skillName,
			Rationale:        fmt.Sprintf("Promoted from learned pattern (confidence=%.2f, use_count=%d): %s", p.Confidence, p.UseCount, p.Description),
			CandidateContent: candidateContent,
		}

		e.processProposal(ctx, report, proposal, "", "create_skill")
	}
}

// dedupePatternSkillName appends a numeric suffix (-2, -3, ...) to baseName if
// it already exists in existingNames or proposedNames. Cap iterations at 20 to
// avoid pathological loops; if all 20 are taken, returns the last candidate
// (the writer's dedup guard will refuse to overwrite an identical content SHA).
func dedupePatternSkillName(baseName string, existingNames, proposedNames map[string]bool) string {
	if !existingNames[baseName] && !proposedNames[baseName] {
		return baseName
	}
	for i := 2; i <= 20; i++ {
		candidate := fmt.Sprintf("%s-%d", baseName, i)
		if !existingNames[candidate] && !proposedNames[candidate] {
			return candidate
		}
	}
	// All 20 slots taken; return a name that will likely collide and be
	// skipped by the writer's SHA dedup check rather than overwriting.
	return fmt.Sprintf("%s-%d", baseName, 21)
}

// buildSkillFromPatternNamed creates a minimal SKILL.md body from a learned
// pattern using an explicit skill name (typically produced by
// dedupePatternSkillName to avoid collisions). Callers that want the legacy
// behavior of deriving the name from the pattern can call
// patternToSkillName(p) first and pass the result.
func buildSkillFromPatternNamed(p *selfimprove.LearnedPattern, skillName string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", skillName))
	sb.WriteString("description: " + p.Description + "\n")
	if len(p.Tags) > 0 {
		sb.WriteString("tags: [" + strings.Join(p.Tags, ", ") + "]\n")
	}
	sb.WriteString("---\n\n")
	sb.WriteString("# " + strings.ToTitle(skillName) + "\n\n")
	sb.WriteString(p.Description + "\n\n")
	sb.WriteString("## Pattern\n\n")
	sb.WriteString(p.Pattern + "\n")
	if len(p.Examples) > 0 {
		sb.WriteString("\n## Examples\n\n")
		for _, ex := range p.Examples {
			sb.WriteString("- " + ex + "\n")
		}
	}
	return sb.String()
}

// patternToSkillName converts a learned pattern into a kebab-case skill name.
func patternToSkillName(p *selfimprove.LearnedPattern) string {
	name := p.Description
	if len(name) > 40 {
		name = name[:40]
	}
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, name)
	// Collapse multiple dashes and trim edges.
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if name == "" {
		name = "promoted-skill"
	}
	if p.Domain != "" {
		name = p.Domain + "-" + name
	}
	return name
}

// ---------------------------------------------------------------------------
// Pass C: Prune low-performing skills
// ---------------------------------------------------------------------------

// passCPrune identifies skills with effectiveness below the configured
// threshold and proposes archiving them.
func (e *Evolver) passCPrune(ctx context.Context, report *EvolutionReport) {
	if e.usage == nil {
		e.logger.Debug("pass C (prune) skipped: usage tracker not configured")
		return
	}

	lowPerformers, err := e.usage.GetLowPerformers(e.cfg.MinEffectiveness, 10)
	if err != nil {
		e.logger.Warn("pass C: failed to get low performers", "error", err)
		return
	}

	for _, stats := range lowPerformers {
		currentContent := ""
		if e.writer != nil {
			if content, err := e.writer.ReadSkill(stats.SkillName); err == nil {
				currentContent = content
			}
		}

		proposal := EvolutionProposal{
			Action:           ProposalArchive,
			SkillName:        stats.SkillName,
			Rationale:        fmt.Sprintf("Low effectiveness %.2f over %d injections", stats.Effectiveness, stats.InjectCount),
			CandidateContent: "",
		}

		e.processProposal(ctx, report, proposal, currentContent, "archive_skill")
	}
}

// ---------------------------------------------------------------------------
// Proposal processing (shared by all three passes)
// ---------------------------------------------------------------------------

// processProposal is the central decision-and-apply pipeline shared by all
// three passes. It routes every proposal through the verifier, then either
// applies it (AutoApply=true) or creates a plan (AutoApply=false).
func (e *Evolver) processProposal(ctx context.Context, report *EvolutionReport, proposal EvolutionProposal, currentContent, verifyAction string) {
	// Route through the verifier gate.
	verifyReq := VerifyRequest{
		Action:           verifyAction,
		SkillName:        proposal.SkillName,
		CandidateContent: proposal.CandidateContent,
		CurrentContent:   currentContent,
		EvidenceSummary:  proposal.Rationale,
	}

	vr, err := e.verifier.Verify(ctx, verifyReq)
	if err != nil {
		e.logger.Warn("verifier error, skipping proposal",
			"skill", proposal.SkillName, "error", err)
		report.Skipped++
		proposal.VerifierResult = nil
		report.Details = append(report.Details, proposal)
		return
	}

	proposal.VerifierResult = vr

	if vr.Action != ActionAccept {
		report.Rejected++
		report.Details = append(report.Details, proposal)
		e.logger.Info("proposal rejected by verifier",
			"skill", proposal.SkillName,
			"action", proposal.Action,
			"score", vr.Score,
			"reasons", vr.Reasons,
		)
		return
	}

	// Verifier accepted — apply or plan.
	if e.cfg.AutoApply {
		if err := e.applyProposal(ctx, proposal); err != nil {
			e.logger.Error("failed to apply proposal",
				"skill", proposal.SkillName, "action", proposal.Action, "error", err)
			report.Skipped++
		} else {
			e.incrementActionCount(report, proposal.Action)
		}
	} else {
		// AutoApply disabled: create a plan (if plan manager is available).
		if e.planMgr != nil {
			planTitle := fmt.Sprintf("Skill evolution: %s %s", proposal.Action, proposal.SkillName)
			planDesc := proposal.Rationale
			if proposal.CandidateContent != "" {
				planDesc += "\n\nCandidate content:\n" + proposal.CandidateContent
			}
			if _, perr := e.planMgr.CreatePlan(ctx, planTitle, planDesc, "", "", ""); perr != nil {
				e.logger.Error("failed to create plan for proposal",
					"skill", proposal.SkillName, "error", perr)
				report.Skipped++
			} else {
				report.Planned++
			}
		} else {
			// No plan manager: record as "would have created plan" without applying.
			e.logger.Info("auto-apply disabled and no plan manager; recording proposal without applying",
				"skill", proposal.SkillName, "action", proposal.Action)
			report.Planned++
		}
	}

	report.Details = append(report.Details, proposal)
}

// incrementActionCount bumps the appropriate report counter for a successfully
// applied proposal.
func (e *Evolver) incrementActionCount(report *EvolutionReport, action EvolutionProposalAction) {
	switch action {
	case ProposalRefine:
		report.Refined++
	case ProposalCreate:
		report.Promoted++
	case ProposalArchive:
		report.Pruned++
	}
}

// applyProposal writes or archives the skill on disk. For refine and create,
// it calls Writer.WriteSkill (which snapshots + dedup-checks + writes), then
// re-parses and registers the skill so it is immediately discoverable. For
// archive, it calls Writer.ArchiveSkill.
func (e *Evolver) applyProposal(ctx context.Context, proposal EvolutionProposal) error {
	switch proposal.Action {
	case ProposalRefine, ProposalCreate:
		if e.writer == nil {
			return fmt.Errorf("evolver: writer not configured")
		}
		if err := e.writer.WriteSkill(proposal.SkillName, proposal.CandidateContent); err != nil {
			return fmt.Errorf("evolver: write skill %s: %w", proposal.SkillName, err)
		}
		// Re-parse and register so the skill is immediately discoverable.
		if e.registry != nil {
			skillPath := e.writer.skillPath(proposal.SkillName)
			if parsed, err := skills.ParseSkillFile(skillPath); err == nil {
				e.registry.Register(parsed)
			} else {
				e.logger.Warn("failed to re-parse written skill",
					"skill", proposal.SkillName, "error", err)
			}
		}
		return nil

	case ProposalArchive:
		if e.writer == nil {
			return fmt.Errorf("evolver: writer not configured")
		}
		if err := e.writer.ArchiveSkill(proposal.SkillName); err != nil {
			return fmt.Errorf("evolver: archive skill %s: %w", proposal.SkillName, err)
		}
		return nil

	default:
		return fmt.Errorf("evolver: unknown proposal action %q", proposal.Action)
	}
}

// ---------------------------------------------------------------------------
// LLM helper
// ---------------------------------------------------------------------------

// callLLMJSON sends a system+user prompt to the LLM and parses the response as
// JSON. Returns nil map (not error) if the LLM client is nil — callers should
// treat nil as "no decision" and skip.
func (e *Evolver) callLLMJSON(ctx context.Context, systemPrompt, userPrompt string) (map[string]any, error) {
	if e.llmClient == nil {
		return nil, nil
	}

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: userPrompt},
	}

	resp, err := e.llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	jsonData := markdown.ExtractJSON(resp.Content)
	if jsonData == nil {
		return nil, fmt.Errorf("no valid JSON in LLM response")
	}

	var result map[string]any
	if err := json.Unmarshal(jsonData, &result); err != nil {
		return nil, fmt.Errorf("unmarshal LLM response: %w", err)
	}

	return result, nil
}
