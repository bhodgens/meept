package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

const defaultAmbiguityThreshold = 0.6

// TrueIntentAnalysis represents a deep analysis of the user's actual intent.
type TrueIntentAnalysis struct {
	Goal               string   `json:"goal"`
	Ambiguity          float64  `json:"ambiguity"`
	Scope              string   `json:"scope"`
	Category           string   `json:"category"`
	SuggestedQuestions []string `json:"suggested_questions"`
	Confidence         float64  `json:"confidence"`
	SuggestedMode      string   `json:"suggested_mode,omitempty"`
	// ApprovalRequested is the C-3 (plan-contract tree
	// 20261125-plan-contract-quickplan, decision D3) pre-execution approval
	// flag: true ONLY when the user explicitly asked to review/approve the
	// plan BEFORE any execution happens ("implement it only after I
	// approve"). Absent/false is the legacy oneshot posture — a quickplan
	// turn auto-approves and executes immediately. omitempty keeps the
	// re-marshaled JSON of a false analysis byte-identical to pre-C-3.
	ApprovalRequested bool `json:"approval_requested,omitempty"`
}

// IsAmbiguous returns true if the ambiguity score meets or exceeds the threshold.
func (a *TrueIntentAnalysis) IsAmbiguous(threshold float64) bool {
	return a.Ambiguity >= threshold
}

// IntentAnalyzer wraps an LLM client to perform deep intent analysis.
type IntentAnalyzer struct {
	client             *llm.Client
	ambiguityThreshold float64
	logger             *slog.Logger

	// tokenCap is the per-call output token budget, derived from the
	// model's declared max_output (see effectiveClassificationCap).
	tokenCap int

	// resolver enables alias-based failover (leaf 03 of
	// classifier-reliability). When non-nil (with aliasName set), a failed
	// Chat attempt records an alias failure, rotates to the next candidate,
	// reconfigures the client, and retries once.
	resolver  *llm.Resolver // nil = no failover (legacy behavior)
	aliasName string        // e.g. "classifier"; required when resolver != nil

	// modelConfig is the resolved model configuration for the analyzer's
	// endpoint; passed to RecordAliasFailure for failure attribution
	// (issue #30). Nil is allowed — attribution is skipped.
	modelConfig *llm.ModelConfig

	// scopeProvider, when set, supplies the owning loop's task/session
	// scope for LLM calls (leaf 01 session-affinity).
	scopeProvider chatScopeProvider

	// piiMask is the session-scoped PII mask lane applied to user-derived
	// content before the analysis send (finding C21: the main-turn mask
	// in loop.go never reaches secondary classifier sends). Nil = masking
	// disabled, byte-identical. Set via SetPiiMaskHook.
	piiMask *PiiRestoreHook

	// servedModel is the resolved "provider/model" of the endpoint the
	// analyzer is currently configured to serve from (provenance, leaf 01
	// of classifier-observability). Initialized from the constructor's
	// model config and refreshed whenever alias failover reconfigures the
	// client, so ResolvedModel() reports the model that ACTUALLY served.
	servedModel string

	// failFast disables alias rotation (classifier-observability leaf 02):
	// when true, chatWithFailover returns the primary error immediately on
	// failure instead of recording an alias failure and rotating. Default
	// false — production keeps rotation.
	failFast bool

	// bootReadiness/bootWait mirror LLMClassifier's boot-gate wait (see
	// classifier_boot_readiness.go): the analyzer dials the SAME local
	// classifier endpoint, so an analysis call inside the daemon's boot
	// window must wait for the F-D8 gate too instead of failing into the
	// degraded chain.
	bootReadiness *ClassifierBootReadiness
	bootWait      time.Duration
}

// IntentAnalyzerConfig carries optional construction parameters for
// newIntentAnalyzer. All fields are additive; zero values keep prior defaults.
type IntentAnalyzerConfig struct {
	// ModelConfig is the resolved model configuration for the analyzer's
	// endpoint. When non-nil, the per-call token cap is derived from its
	// declared max_output (see effectiveClassificationCap).
	ModelConfig *llm.ModelConfig
	// Resolver enables alias failover (leaf 03 of classifier-reliability).
	// When non-nil, a failed Chat attempt records an alias failure, rotates
	// to the next candidate via ResolveForAlias(aliasName), swaps the client
	// config, and retries once. Nil = no failover (unchanged behavior).
	Resolver *llm.Resolver
	// AliasName is the resolver alias used for failover (e.g. "classifier").
	// Required when Resolver != nil; ignored otherwise.
	AliasName string
	// FailFast disables alias rotation: when true, a failed primary attempt
	// returns the primary error immediately instead of rotating to weaker
	// alias members. Default false — production keeps rotation. Exists to
	// make classifier failures honest during testing/iteration
	// (classifier-observability leaf 02).
	FailFast bool
	// ScopeProvider, when set, supplies the owning loop's task/session
	// scope for analysis LLM calls (leaf 01 session-affinity). Mirrors
	// LLMClassifierConfig.ScopeProvider. Per-call scopes attached via
	// WithScopeOptions on the context take precedence. Nil = unscoped
	// calls, byte-identical to prior behavior.
	ScopeProvider chatScopeProvider
}

// newIntentAnalyzerWithConfig is the fully-parameterized constructor used by
// the dispatcher to wire resolver-based alias failover.
func newIntentAnalyzerWithConfig(cfg IntentAnalyzerConfig, client *llm.Client, logger *slog.Logger) *IntentAnalyzer {
	if logger == nil {
		logger = slog.Default()
	}
	return &IntentAnalyzer{
		client:             client,
		ambiguityThreshold: defaultAmbiguityThreshold,
		logger:             logger,
		tokenCap:           effectiveClassificationCap(cfg.ModelConfig),
		resolver:           cfg.Resolver,
		aliasName:          cfg.AliasName,
		modelConfig:        cfg.ModelConfig,
		failFast:           cfg.FailFast,
		scopeProvider:      cfg.ScopeProvider,
		servedModel:        resolvedModelID(cfg.ModelConfig),
	}
}

// resolvedModelID formats a model config as its "provider/model" provenance
// id. A nil config (or missing provider id) yields "" — unknown provenance
// must stay empty, not be faked (leaf 01 of classifier-observability).
func resolvedModelID(cfg *llm.ModelConfig) string {
	if cfg == nil || cfg.ProviderID == "" {
		return ""
	}
	return cfg.ProviderID + "/" + cfg.ModelID
}

// ResolvedModel returns the resolved "provider/model" of the LLM that served
// the analyzer's calls (provenance, leaf 01 of classifier-observability). It
// reflects the model the client is currently configured with, including any
// alias-failover rotation performed by chatWithFailover. Empty when the
// serving model is unknown (no model config, or a config without a provider
// id) — consumers must treat empty as "no provenance", never fabricate one.
func (ia *IntentAnalyzer) ResolvedModel() string {
	return ia.servedModel
}

// WithAmbiguityThreshold sets a custom ambiguity threshold.
func (ia *IntentAnalyzer) WithAmbiguityThreshold(threshold float64) *IntentAnalyzer {
	ia.ambiguityThreshold = threshold
	return ia
}

// intentAnalysisSystemPrompt is the system prompt sent with every intent
// analysis call. The final two rules (session activity) are context-
// conditioned ambiguity rules: they are present in EVERY call and are
// harmless when no session activity is provided.
const intentAnalysisSystemPrompt = `You are an intent analysis assistant. Analyze the user's input and return ONLY valid JSON with these exact fields:
- goal (string): What the user actually wants
- ambiguity (number 0.0-1.0): How ambiguous the request is (1.0 = very ambiguous)
- scope (string): One of "narrow", "medium", "broad"
- category (string): One of "research", "implementation", "investigation", "fix", "quickplan", "clarification", "other"
- suggested_questions (array of strings): If ambiguity >= 0.6, list clarifying questions to ask the user; otherwise empty array
- confidence (number 0.0-1.0): Your confidence in this analysis
- suggested_mode (string): One of "direct", "plan", "quick_plan", "spec_plan", "spec_pair"
  - "direct" for trivial/lookup questions
  - "plan" for single-component work
  - "quick_plan" for "just do it" execution of work without check-ins (clarify only if truly blocked)
  - "spec_plan" for multi-file or multi-phase work
  - "spec_pair" for compound requests

Rules:
- scope must be exactly "narrow", "medium", or "broad"
- category must be exactly one of the allowed values
- suggested_mode must be exactly one of the allowed values
- approval_requested (boolean, optional): set it true ONLY when the user explicitly asks to review or approve the plan BEFORE any execution happens. Examples: "plan the refactoring and implement it only after I approve" => true. Requests to plan and then act, and document-only requests, are false: "write a plan for X and create a markdown file" => false, "plan X then implement it" => false. When in doubt, omit it (false).
- Keep the response concise.
- Recent session activity may be provided with the input. Use it to resolve pronouns and references ('the change', 'the file', 'it') against what was just done.
- If the input is a short follow-up question about the recent activity and the activity makes the referent clear, set ambiguity LOW and proceed — do not ask the user to re-specify.`

// buildActivityBlock formats the [Recent session activity] appendix from the
// contract (leaf 02 of session-aware-intent-gate). Callers must only invoke
// this with a non-empty digest. state/agent segments are omitted when empty;
// the Result summary line is omitted when the summary is empty.
func buildActivityBlock(d *SessionContextDigest) string {
	var sb strings.Builder
	sb.WriteString("\n\n[Recent session activity]\n")
	sb.WriteString("Last task: ")
	sb.WriteString(d.LastTaskName)
	if d.LastTaskState != "" || d.LastTaskAgent != "" {
		segments := make([]string, 0, 2)
		if d.LastTaskState != "" {
			segments = append(segments, "state: "+d.LastTaskState)
		}
		if d.LastTaskAgent != "" {
			segments = append(segments, "agent: "+d.LastTaskAgent)
		}
		sb.WriteString(" (")
		sb.WriteString(strings.Join(segments, ", "))
		sb.WriteString(")")
	}
	if d.LastResultSummary != "" {
		sb.WriteString("\nResult summary: ")
		sb.WriteString(d.LastResultSummary)
	}
	// Leaf 03: append the working directory line only when set, so the
	// block never ends with a dangling newline when it is absent.
	if d.WorkingDirectory != "" {
		sb.WriteString("\nWorking directory: ")
		sb.WriteString(d.WorkingDirectory)
	}
	return sb.String()
}

// buildAnalysisMessages constructs the chat messages for intent analysis.
// A nil or empty digest yields exactly the contextless messages
// [{system, intentAnalysisSystemPrompt}, {user, input}] — byte-identical to
// pre-session behavior. A non-empty digest appends the [Recent session
// activity] block to the user message.
func (ia *IntentAnalyzer) buildAnalysisMessages(input string, sessionContext *SessionContextDigest) []llm.ChatMessage {
	userContent := input
	if !sessionContext.IsEmpty() {
		userContent += buildActivityBlock(sessionContext)
	}
	return []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: intentAnalysisSystemPrompt},
		{Role: llm.RoleUser, Content: userContent},
	}
}

// awaitBootReadiness mirrors LLMClassifier's boot-gate wait: the analyzer
// dials the same local classifier endpoint, so a call inside the daemon's
// boot window waits (bounded, cancellable, never failing) for the F-D8 gate.
func (ia *IntentAnalyzer) awaitBootReadiness(ctx context.Context) {
	awaitClassifierBoot(ctx, &slogAdapter{l: ia.logger}, ia.bootReadiness, ia.bootWait, "intent-analyzer")
}

// SetBootReadiness wires the daemon's F-D8 classifier boot-gate signal onto
// an already-constructed analyzer. Nil is a no-op (setter nil-guard rule).
func (ia *IntentAnalyzer) SetBootReadiness(b *ClassifierBootReadiness) {
	if ia == nil || b == nil {
		return
	}
	ia.bootReadiness = b
	if ia.bootWait <= 0 {
		ia.bootWait = defaultClassifierBootWait
	}
}

// SetPiiMaskHook attaches the session-scoped PII mask lane used for the
// analysis side send (finding C21). Nil is a no-op (setter nil-guard rule):
// with no hook the raw content passes through byte-identically.
func (ia *IntentAnalyzer) SetPiiMaskHook(hook *PiiRestoreHook) {
	if ia == nil || hook == nil {
		return
	}
	ia.piiMask = hook
}

// AnalyzeTrueIntent performs a lightweight LLM-based analysis of the user's
// true intent. sessionContext, when non-nil and non-empty, appends a compact
// [Recent session activity] block to the user message so pronouns and
// references ("the change", "it") can be resolved against recent work; a nil
// or empty digest keeps the prompt byte-identical to the contextless form.
func (ia *IntentAnalyzer) AnalyzeTrueIntent(ctx context.Context, input string, sessionContext *SessionContextDigest) (*TrueIntentAnalysis, error) {
	if ia.client == nil {
		return nil, fmt.Errorf("intent analyzer: no client configured")
	}

	ia.awaitBootReadiness(ctx)
	messages := ia.buildAnalysisMessages(input, sessionContext)
	// Finding C21: mask PII in the user-derived content before the side
	// send crosses to the provider (same lane as the main turn; session
	// scope resolves through the owning loop's scope provider). Nil hook
	// or empty session -> messages unchanged, byte-identical.
	messages = maskSideSendMessages(ia.piiMask, messages, sideSendSessionID(ia.scopeProvider))

	opts := resolveCallScope(ctx, ia.scopeProvider, []llm.ChatOption{
		llm.WithMaxTokens(ia.tokenCap),
		llm.WithTemperature(0.2),
		noThinkingOpt(),
	})
	resp, err := ia.chatWithFailover(ctx, messages, opts...)
	if err != nil {
		ia.logger.Warn("intent analysis failed", "error", err)
		return nil, fmt.Errorf("intent analysis failed: %w", err)
	}

	analysis, err := ia.parseAnalysis(resp.Content)
	if err != nil {
		return nil, err
	}
	// C-3 B-fallback (decision D3): when the input looks like an explicit
	// pre-execution approval request (cheap trigger, never a verdict) but
	// the primary analysis degraded to the default false, ask the same
	// endpoint ONE bounded yes/no adjudication. Any failure keeps false
	// (oneshot); adjudication never parks or fails the turn. A primary
	// true vote short-circuits: the model already answered, no second
	// spend.
	if !analysis.ApprovalRequested && vetoSuspectTrigger(input) {
		if ia.adjudicateApproval(ctx, input) {
			analysis.ApprovalRequested = true
			ia.logger.Debug("approval adjudication overrode the degraded false vote",
				"trigger", "veto_suspect")
		}
	}
	return analysis, nil
}

// approvalAdjudicationTimeout bounds the C-3 B-fallback call: one small
// yes/no question on the classifier endpoint. A slow adjudicator must never
// stall the turn — the deadline expiring keeps the oneshot posture.
const approvalAdjudicationTimeout = 5 * time.Second

// vetoSuspectApprovalRe is half of the C-3 veto-suspect TRIGGER: the
// approval vocabulary ("approve", "sign-off", "go-ahead"). It is a cheap
// prefilter that decides whether to SPEND one bounded adjudication call —
// it never produces a verdict itself (D3: the regex dies, adjudication
// decides).
var vetoSuspectApprovalRe = regexp.MustCompile(`(?i)\b(approve|approv\w*|sign[-\s]?off|go[-\s]?ahead|green[-\s]?light|authoriz\w*)\b`)

// vetoSuspectTimingRe is the trigger's other half: pre-execution ordering
// vocabulary (before/wait/until/only after/hold off/first). Both halves
// must co-occur; approval words alone ("is approval enabled?") or timing
// words alone ("wait for the build") do not fire the adjudication.
var vetoSuspectTimingRe = regexp.MustCompile(`(?i)\b(before|first|wait|holding?\s+off|until|only\s+after)\b`)

// vetoSuspectTrigger reports whether an input carries the SHAPE of an
// explicit pre-execution approval request: approval vocabulary joined to
// pre-execution timing vocabulary. Trigger only — the verdict comes from
// the analyzer's own approval_requested field or, when that degraded to
// false, one bounded adjudication call.
func vetoSuspectTrigger(input string) bool {
	if input == "" {
		return false
	}
	return vetoSuspectApprovalRe.MatchString(input) && vetoSuspectTimingRe.MatchString(input)
}

// approvalAdjudicationSystemPrompt is the B-fallback's whole job: answer the
// single LANGUAGE question the deleted autoSeal regexes used to answer
// (badly: bughunt M6 polarity escapes) with one JSON boolean.
const approvalAdjudicationSystemPrompt = `You are a phrasing adjudicator. Decide ONE question about the user's request: does the user explicitly ask to review or approve the plan BEFORE any execution happens (e.g. "implement it only after I approve", "hold off until I sign off")?
Return ONLY this JSON object: {"approval_required": true|false}
Rules:
- true ONLY for an explicit pre-execution approval request by the user.
- Planning-then-acting ("plan X then implement it") is false.
- Document-only or plain execution requests are false.
- Negation alone ("don't implement it") is false: it refuses execution, it does not request an approval gate.
- When in doubt, return false.`

// adjudicateApproval performs the bounded B-fallback call (C-3/D3): reuses
// the analyzer's own client, a derived ctx deadline, and the same side-send
// mask lane as the primary analysis. ANY failure (transport, empty,
// unparseable) keeps the oneshot posture (false) with one Debug line — the
// adjudication never parks or fails a turn.
func (ia *IntentAnalyzer) adjudicateApproval(ctx context.Context, input string) bool {
	if ia.client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, approvalAdjudicationTimeout)
	defer cancel()

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: approvalAdjudicationSystemPrompt},
		{Role: llm.RoleUser, Content: input},
	}
	messages = maskSideSendMessages(ia.piiMask, messages, sideSendSessionID(ia.scopeProvider))

	resp, err := ia.client.Chat(ctx, messages,
		llm.WithMaxTokens(64),
		llm.WithTemperature(0.0),
		noThinkingOpt(),
	)
	if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
		ia.logger.Debug("approval adjudication degraded; keeping oneshot posture", "error", err)
		return false
	}
	jsonStr := extractJSONFromLLM(resp.Content)
	if jsonStr == "" {
		ia.logger.Debug("approval adjudication: no JSON in reply; keeping oneshot posture")
		return false
	}
	var verdict struct {
		ApprovalRequired bool `json:"approval_required"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &verdict); err != nil {
		ia.logger.Debug("approval adjudication: unparseable verdict; keeping oneshot posture", "error", err)
		return false
	}
	return verdict.ApprovalRequired
}

// chatWithFailover performs at most two Chat attempts against the underlying
// client: the initial attempt plus, when resolver-based alias failover is
// configured and the first attempt fails (including empty responses), one
// rotation to the next alias candidate. On success it records AliasSuccess so
// resolver health resets. Max 2 total attempts — no loops.
func (ia *IntentAnalyzer) chatWithFailover(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	attempt := func() (*llm.Response, error) {
		resp, err := ia.client.Chat(ctx, messages, opts...)
		if err != nil {
			return nil, err
		}
		if resp == nil || strings.TrimSpace(resp.Content) == "" {
			return nil, fmt.Errorf("intent analysis: %w", llm.ErrEmptyResponse)
		}
		return resp, nil
	}

	resp, err := attempt()
	if err == nil {
		if ia.resolver != nil && ia.aliasName != "" {
			// Identity-attributed success clear (bughunt 2026-09-08 item
			// 14): modelConfig identifies the model that served this
			// attempt (nil = unresolvable → alias-wide clear), so a
			// straggler success cannot erase another model's earned
			// cooldown/block.
			ia.resolver.RecordAliasSuccessModel(ia.aliasName, ia.modelConfig)
		}
		return resp, nil
	}
	// Classifier fail-fast (classifier-observability leaf 02): when
	// configured, surface the primary's error honestly — no alias-failure
	// recording, no rotation, no retry.
	if ia.failFast {
		ia.logger.Warn("classifier fail-fast: no rotation (configured)",
			"alias", ia.aliasName,
			"error", err,
		)
		return nil, err
	}
	if ia.resolver == nil || ia.aliasName == "" {
		return nil, err
	}

	// Fail over: record the failure, advance to the next candidate, swap the
	// client config, and retry once. ModelConfig identifies the model the
	// failed attempt was served by (nil when unresolvable — see issue #30).
	ia.resolver.RecordAliasFailure(ia.aliasName, err, ia.modelConfig)
	nextCfg, rerr := ia.resolver.ResolveForAlias(ia.aliasName, "")
	if rerr != nil || nextCfg == nil {
		return nil, fmt.Errorf("intent analysis: %w (no alternate candidate: %w)", err, rerr)
	}
	ia.logger.Warn("Intent analyzer rotating to next alias candidate",
		"alias", ia.aliasName,
		"model", nextCfg.ProviderID+"/"+nextCfg.ModelID,
		"error", err,
	)
	ia.client.Reconfigure(nextCfg)
	ia.modelConfig = nextCfg
	// Provenance tracking (leaf 01 of classifier-observability): from here
	// on, ResolvedModel() must report the rotated candidate — the model
	// that actually serves the analysis.
	ia.servedModel = resolvedModelID(nextCfg)

	resp, err = attempt()
	if err == nil {
		// Post-rotation success: modelConfig was swapped to the serving
		// candidate above, so the clear is identity-attributed (bughunt
		// 2026-09-08 item 14). Nil modelConfig degrades to alias-wide.
		ia.resolver.RecordAliasSuccessModel(ia.aliasName, ia.modelConfig)
	}
	return resp, err
}

func (ia *IntentAnalyzer) parseAnalysis(content string) (*TrueIntentAnalysis, error) {
	jsonStr := extractJSONFromLLM(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("intent analysis: no JSON found in response")
	}

	var analysis TrueIntentAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil, fmt.Errorf("intent analysis: failed to parse JSON: %w", err)
	}

	// Normalize and validate
	analysis.Scope = strings.ToLower(strings.TrimSpace(analysis.Scope))
	analysis.Category = strings.ToLower(strings.TrimSpace(analysis.Category))

	validScopes := map[string]bool{"narrow": true, "medium": true, "broad": true}
	if !validScopes[analysis.Scope] {
		analysis.Scope = "medium"
	}

	validCategories := map[string]bool{
		"research": true, "implementation": true, "investigation": true,
		"fix": true, "clarification": true, "other": true,
		// quickplan-mode leaf 01: quickplan is the 13th intent class; the
		// ambiguity gate checks it to seed PendingMode on clarifications.
		string(IntentQuickPlan): true,
	}
	if !validCategories[analysis.Category] {
		analysis.Category = "other"
	}

	analysis.Ambiguity = clampFloat(analysis.Ambiguity, 0.0, 1.0)
	analysis.Confidence = clampFloat(analysis.Confidence, 0.0, 1.0)

	// Validate suggested_mode (Thread D complexity routing). Invalid/empty
	// values are zeroed — the rule-based fallback in suggestMode handles it.
	analysis.SuggestedMode = validateMode(strings.ToLower(strings.TrimSpace(analysis.SuggestedMode)))

	return &analysis, nil
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
