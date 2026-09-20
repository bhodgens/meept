package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
)

// noThinkingOpt returns a Chat option that explicitly disables thinking for
// small classification calls (leaf 01 of classifier-reliability): thinking
// models would otherwise burn the token cap on reasoning and return empty
// content.
func noThinkingOpt() llm.ChatOption {
	noThinking := false
	return llm.WithReasoning(&llm.ReasoningConfig{Enabled: &noThinking})
}

const (
	// defaultClassifierTimeout is used when LLMClassifierConfig.Timeout is zero.
	defaultClassifierTimeout = 10 * time.Second
	// defaultUnavailableCooldown is how long the classifier treats the endpoint
	// as unavailable after a failure, before retrying.
	defaultUnavailableCooldown = 60 * time.Second
)

// classifierLanes is the single source of truth for the intents the LLM
// classifier may emit. It drives the classifier prompts, the validity gate
// (isValidIntent), and - through agentMapping and IntentType.DefaultAgent -
// the agent each lane routes to.
//
// Why one list: the lane list was duplicated as a hard-coded string in the
// classifier prompt, again in the multi-intent prompt, and AGAIN in the
// isValidIntent gate. They drifted. IntentQuickPlan shipped with a dispatcher
// route, an agent mapping, and 58 gold evaluation cases, but was absent from
// all three lists - so the LLM could never emit it and every "just do it"
// prompt landed on a wrong lane (2026-09-12: a tool-invocation prompt
// classified as platform at 0.9). IntentResearch and IntentToolUse were
// missing from the gate for the same reason, which made the research lane
// unreachable from the LLM classifier entirely.
//
// Order is deliberate: specific lanes first, the conversational catch-all
// last, so the model reads the meaningful options before "chat".
var classifierLanes = []IntentType{
	IntentGit,
	IntentSchedule,
	IntentCode,
	IntentDebug,
	IntentReview,
	IntentPlan,
	IntentQuickPlan,
	IntentPlatform,
	IntentReport,
	IntentRecall,
	IntentAnalyze,
	IntentSearch,
	IntentResearch,
	IntentExplore,
	IntentToolUse,
	IntentSecurity,
	IntentStatus,
	IntentWrite,
	IntentArchitect,
	IntentSkeptic,
	IntentLibrarian,
	IntentImageGen,
	IntentVideoGen,
	IntentImageID,
	IntentInstruction,
	IntentClarify,
	IntentChat,
}

// classifierLaneSet is the membership set for isValidIntent, derived from
// classifierLanes so the two can never disagree.
var classifierLaneSet = func() map[string]bool {
	set := make(map[string]bool, len(classifierLanes))
	for _, lane := range classifierLanes {
		set[string(lane)] = true
	}
	return set
}()

// laneList renders classifierLanes as the "a, b, c" list embedded in the
// classifier prompts.
func laneList() string {
	names := make([]string, 0, len(classifierLanes))
	for _, lane := range classifierLanes {
		names = append(names, string(lane))
	}
	return strings.Join(names, ", ")
}

// laneAgentIndex is the frontmatter-derived lane -> agent-ID routing index.
// It is the PRIMARY destination for agentForIntent: it is built from the
// `intents:` list in each AGENT.md definition (see BuildLaneAgentIndexFromDir
// and AgentRegistry.publishLaneIndex), so adding a new specialist agent is a
// frontmatter-only change with no Go edit. agentMapping and each lane's
// IntentType.DefaultAgent remain ordered fallbacks for lanes that no agent
// declares. Stored as an atomic pointer so reads are lock-free and a
// republish is a single Store that can never expose a half-built map.
var laneAgentIndex atomic.Pointer[map[string]string]

// agentForIntent maps a lane to its agent. Resolution order:
//
//  1. the frontmatter-derived laneAgentIndex - any agent whose AGENT.md
//     declares the lane in `intents:` is authoritative;
//  2. the static agentMapping table (backward compatibility);
//  3. the lane's own IntentType.DefaultAgent;
//  4. the chat agent as the final safety net.
//
// Steps 2-4 keep existing behavior intact when no agent declares a lane, so
// the dynamic path can be adopted incrementally without regressing routing.
func agentForIntent(intent string) string {
	if idx := laneAgentIndex.Load(); idx != nil {
		if agent, ok := (*idx)[intent]; ok && agent != "" {
			return agent
		}
	}
	if agent, ok := agentMapping[intent]; ok && agent != "" {
		return agent
	}
	if agent := IntentType(intent).DefaultAgent(); agent != "" {
		return agent
	}
	return config.AgentIDChat
}

// LaneRoute is one row of the routing table: a classifier lane and the agent
// it routes to.
type LaneRoute struct {
	Intent string `json:"intent"`
	Agent  string `json:"agent"`
}

// LaneAgentFor resolves one lane to its agent using the same order as the
// classifier: the frontmatter-derived index, then the static table, then the
// lane's own default. Exported so callers outside this package (the CLI's
// `meept lanes` command, tests) read the routing decision rather than a copy
// of the tables.
func LaneAgentFor(lane string) string {
	return agentForIntent(lane)
}

// LaneAgentTable returns the canonical lane -> agent table in classifierLanes
// order. It is the exported face of agentForIntent, so the daemon classifier,
// the `meept lanes` artifact (consumed by the prompt-router sidecar) and any
// doc generator all read the SAME table instead of restating it.
//
// Named LaneAgentTable, not RoutingTable: strategic_routing.go already owns
// that name for its actor/reviewer table.
func LaneAgentTable() []LaneRoute {
	routes := make([]LaneRoute, 0, len(classifierLanes))
	for _, lane := range classifierLanes {
		routes = append(routes, LaneRoute{
			Intent: string(lane),
			Agent:  agentForIntent(string(lane)),
		})
	}
	return routes
}

// BuildLaneAgentIndexFromDir builds a lane -> agent-ID index from the AGENT.md
// definitions under dir. dir may contain AGENT.md files directly, agent
// subdirectories (<dir>/<id>/AGENT.md), or both. Definition paths are visited
// in sorted order so the result is deterministic when two agents declare the
// same lane (the first path sorted wins). Disabled agents (enabled: false) and
// empty lane names are skipped. An empty or nil index means no agent declares
// any lane.
func BuildLaneAgentIndexFromDir(dir string) (map[string]string, error) {
	paths, err := agentDefinitionPaths(dir)
	if err != nil {
		return nil, err
	}
	idx := make(map[string]string)
	for _, path := range paths {
		def, err := agents.ParseAgentFile(path)
		if err != nil {
			return nil, fmt.Errorf("lane agent index: parse %s: %w", path, err)
		}
		if !def.IsEnabled() {
			continue
		}
		for _, lane := range def.Intents {
			lane = strings.TrimSpace(lane)
			if lane == "" {
				continue
			}
			if _, exists := idx[lane]; !exists {
				idx[lane] = def.ID
			}
		}
	}
	return idx, nil
}

// agentDefinitionPaths lists the AGENT.md files under dir in sorted order.
// A directory entry contributes <dir>/<entry>/AGENT.md when that file exists;
// a plain file named AGENT.md contributes itself.
func agentDefinitionPaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("lane agent index: read dir %s: %w", dir, err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			candidate := filepath.Join(dir, entry.Name(), "AGENT.md")
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				paths = append(paths, candidate)
			}
			continue
		}
		if entry.Name() == "AGENT.md" {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// PublishLaneAgentIndex atomically installs idx as the lane routing index. A
// nil or empty map clears it, restoring the static fallback path. The map is
// copied so later caller mutation cannot race with reads.
func PublishLaneAgentIndex(idx map[string]string) {
	if len(idx) == 0 {
		laneAgentIndex.Store(nil)
		return
	}
	snapshot := make(map[string]string, len(idx))
	for lane, agent := range idx {
		snapshot[lane] = agent
	}
	laneAgentIndex.Store(&snapshot)
}

// CurrentLaneAgentIndex returns the installed lane routing index, or nil when
// none is published. The returned map must not be mutated.
func CurrentLaneAgentIndex() map[string]string {
	if p := laneAgentIndex.Load(); p != nil {
		return *p
	}
	return nil
}

var intentThresholds = map[string]float64{
	string(IntentGit):      0.85,
	string(IntentSchedule): 0.80,
	string(IntentCode):     0.75,
	string(IntentDebug):    0.75,
	string(IntentReview):   0.75,
	string(IntentPlan):     0.70,
	string(IntentPlatform): 0.70,
	string(IntentReport):   0.70,
	string(IntentRecall):   0.70,
	string(IntentAnalyze):  0.60,
	string(IntentResearch): 0.55,
	string(IntentSecurity): 0.70,
	string(IntentSearch):   0.60,
	// Chat is the catch-all: an 8B emits 0.9+ confidence on every lane it lands
	// in (run-to-run T1 wandered chat/compound/quickplan), so a routine 0.9
	// chat verdict must NOT outrank a keyword code match on an imperative
	// prompt. 0.85 keeps genuine small talk (high-confidence) while letting
	// the keyword/heuristic chain route imperative work.
	string(IntentChat):     0.85,
}

var agentMapping = map[string]string{
	string(IntentGit):         config.AgentIDCommitter,
	string(IntentSchedule):    config.AgentIDScheduler,
	string(IntentCode):        config.AgentIDCoder,
	string(IntentDebug):       config.AgentIDDebugger,
	string(IntentReview):      config.AgentIDCoder,
	string(IntentPlan):        config.AgentIDPlanner,
	string(IntentPlatform):    config.AgentIDChat,
	string(IntentReport):      config.AgentIDChat,
	string(IntentRecall):      config.AgentIDChat,
	string(IntentAnalyze):     config.AgentIDAnalyst,
	string(IntentSearch):      config.AgentIDAnalyst,
	string(IntentChat):        config.AgentIDChat,
	string(IntentResearch):    config.AgentIDResearcher,
	string(IntentSecurity):    config.AgentIDChat,
	string(IntentSkill):       config.AgentIDChat,
	string(IntentCompound):    config.AgentIDDispatcher,
	string(IntentToolUse):     config.AgentIDCoder,
	string(IntentPair):        config.AgentIDChat,
	string(IntentCollaborate): config.AgentIDAnalyst,
}

type LLMClassifier struct {
	client  *llm.Client
	model   string
	timeout time.Duration
	logger  Logger

	// unavailable tracks whether the classifier endpoint is known-to-be-down.
	// When set, subsequent classification attempts are skipped for the
	// cooldown duration, reducing per-request latency and log noise.
	unavailable  atomicBool
	unavailUntil time.Time
	unavailMu    sync.RWMutex

	cooldown time.Duration // how long to cache "unavailable" (default 60s)

	// tokenCap is the per-classification-call output token budget, derived
	// from the model's declared max_output (see effectiveClassificationCap).
	tokenCap int

	// modelConfig is the resolved model configuration, used to derive caps
	// for calls that don't use the precomputed tokenCap.
	modelConfig *llm.ModelConfig

	// servedModel is the resolved "provider/model" of the endpoint the
	// classifier is currently configured to serve from (provenance, leaf 01
	// of classifier-observability). Initialized from the constructor's
	// model config and refreshed whenever alias failover reconfigures the
	// client, so ResolvedModel() reports the model that ACTUALLY served.
	servedModel string

	// resolver enables alias-based failover (leaf 03 of
	// classifier-reliability). When non-nil (with aliasName set), a failed
	// Chat attempt records an alias failure, rotates to the next candidate,
	// reconfigures the client, and retries once.
	resolver  *llm.Resolver // nil = no failover (legacy behavior)
	aliasName string        // e.g. "classifier"; required when resolver != nil
}

// atomicBool is a sync-compatible boolean backed by atomic.Int32.
type atomicBool struct{ v atomic.Int32 }

func (b *atomicBool) Load() bool { return b.v.Load() == 1 }
func (b *atomicBool) Store(v bool) {
	if v {
		b.v.Store(1)
	} else {
		b.v.Store(0)
	}
}

type Logger interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

type LLMClassifierConfig struct {
	Client  *llm.Client
	Model   string
	Timeout time.Duration // When zero, defaultClassifierTimeout is used.
	Logger  Logger
	// ModelConfig is the resolved model configuration for the classifier
	// endpoint. When non-nil, the per-call token cap is derived from its
	// declared max_output (see effectiveClassificationCap). When nil, the
	// floor applies.
	ModelConfig *llm.ModelConfig
	// Resolver enables alias failover (leaf 03 of classifier-reliability).
	// When non-nil, a failed Chat attempt records an alias failure, rotates
	// to the next candidate via ResolveForAlias(aliasName), swaps the client
	// config, and retries once. Nil = no failover (unchanged behavior).
	Resolver *llm.Resolver
	// AliasName is the resolver alias used for failover (e.g. "classifier").
	// Required when Resolver != nil; ignored otherwise.
	AliasName string
}

func NewLLMClassifier(cfg LLMClassifierConfig, logger *slog.Logger) *LLMClassifier {
	var l Logger
	if cfg.Logger != nil {
		l = cfg.Logger
	} else {
		l = &slogAdapter{logger}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultClassifierTimeout
	}
	return &LLMClassifier{
		client:   cfg.Client,
		model:    cfg.Model,
		timeout:  timeout,
		logger:   l,
		cooldown: defaultUnavailableCooldown,

		tokenCap: effectiveClassificationCap(cfg.ModelConfig),

		modelConfig: cfg.ModelConfig,

		resolver:  cfg.Resolver,
		aliasName: cfg.AliasName,

		servedModel: resolvedModelID(cfg.ModelConfig),
	}
}

// ResolvedModel returns the resolved "provider/model" of the LLM that served
// this classifier's calls (provenance, leaf 01 of classifier-observability).
// It reflects the model the client is currently configured with, including
// any alias-failover rotation. Empty when the serving model is unknown —
// consumers must treat empty as "no provenance", never fabricate one.
func (c *LLMClassifier) ResolvedModel() string {
	return c.servedModel
}

// slogAdapter bridges *slog.Logger to the Logger interface.
type slogAdapter struct {
	l *slog.Logger
}

func (a *slogAdapter) Debug(msg string, args ...any) {
	if a.l != nil {
		a.l.Debug(msg, args...)
	}
}
func (a *slogAdapter) Warn(msg string, args ...any) {
	if a.l != nil {
		a.l.Warn(msg, args...)
	}
}
func (a *slogAdapter) Error(msg string, args ...any) {
	if a.l != nil {
		a.l.Error(msg, args...)
	}
}
func (a *slogAdapter) Info(msg string, args ...any) {
	if a.l != nil {
		a.l.Info(msg, args...)
	}
}

type classificationResponse struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning,omitempty"`
}

func (c *LLMClassifier) Classify(ctx context.Context, input string, memCtx *MemoryContext) (*Intent, error) {
	if c.client == nil {
		return nil, fmt.Errorf("LLM classifier: no client configured")
	}

	// Check if we've cached an "unavailable" status and haven't yet exceeded
	// the cooldown window. With resolver failover configured, the cooldown
	// applies only to the CURRENT candidate: before giving up we rotate to
	// the next alias candidate rather than blocking all fallbacks behind one
	// dead primary (leaf 03).
	if c.unavailable.Load() {
		c.unavailMu.RLock()
		unavailUntil := c.unavailUntil
		c.unavailMu.RUnlock()
		if time.Now().Before(unavailUntil) {
			if c.resolver == nil || c.aliasName == "" {
				// Still within cooldown; skip the connection attempt.
				return nil, fmt.Errorf("LLM classifier unavailable (cooldown, retry after %s)",
					unavailUntil.Truncate(time.Second).Format(time.TimeOnly))
			}
			nextCfg, rerr := c.resolver.ResolveForAlias(c.aliasName, "")
			if rerr != nil || nextCfg == nil {
				return nil, fmt.Errorf("LLM classifier unavailable (cooldown until %s) and no alternate candidate: %w",
					unavailUntil.Truncate(time.Second).Format(time.TimeOnly), rerr)
			}
			c.client.Reconfigure(nextCfg)
			c.unavailable.Store(false)
			c.modelConfig = nextCfg
			// Provenance tracking (leaf 01 of classifier-observability):
			// the rotated candidate is now the serving model.
			c.servedModel = resolvedModelID(nextCfg)
			c.logger.Info("LLM classifier rotated to next alias candidate during cooldown",
				"alias", c.aliasName,
				"model", nextCfg.ProviderID+"/"+nextCfg.ModelID,
			)
		} else {
			// Cooldown expired; allow a retry.
			c.unavailable.Store(false)
		}
	}

	classificationPrompt := c.buildClassificationPrompt(input)
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You are an intent classifier for an AI agent system. Classify user inputs into one of these intents: " + laneList() + ". Return ONLY valid JSON with fields: intent (lowercase), confidence (0.0-1.0), and optional reasoning."},
		{Role: llm.RoleUser, Content: classificationPrompt},
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.chatWithFailover(timeoutCtx, messages,
		llm.WithMaxTokens(c.tokenCap),
		llm.WithTemperature(0.1),
		noThinkingOpt(),
	)
	if err != nil {
		// Cache the failure so future requests skip this endpoint.
		c.unavailable.Store(true)
		c.unavailMu.Lock()
		c.unavailUntil = time.Now().Add(c.cooldown)
		c.unavailMu.Unlock()
		c.logger.Warn("LLM classifier unavailable, falling back to keyword",
			"error", err,
			"retry_after", c.cooldown,
		)
		return nil, err
	}

	// Success: clear the unavailable flag.
	c.unavailable.Store(false)

	return c.parseResponse(resp.Content, input)
}

// chatWithFailover performs at most two Chat attempts against the underlying
// client: the initial attempt plus, when resolver-based alias failover is
// configured and the first attempt fails (including empty responses), one
// rotation to the next alias candidate. On success it records AliasSuccess so
// resolver health resets. Max 2 total attempts — no loops.
func (c *LLMClassifier) chatWithFailover(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	attempt := func() (*llm.Response, error) {
		resp, err := c.client.Chat(ctx, messages, opts...)
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.Content == "" {
			return nil, fmt.Errorf("llm classification: %w", llm.ErrEmptyResponse)
		}
		return resp, nil
	}

	resp, err := attempt()
	if err == nil {
		if c.resolver != nil && c.aliasName != "" {
			// Identity-attributed success clear (bughunt 2026-09-08 item
			// 14): modelConfig identifies the model that served this
			// attempt (nil = unresolvable → alias-wide clear), so a
			// straggler success cannot erase another model's earned
			// cooldown/block.
			c.resolver.RecordAliasSuccessModel(c.aliasName, c.modelConfig)
		}
		return resp, nil
	}
	if c.resolver == nil || c.aliasName == "" {
		return nil, err
	}

	// Fail over: record the failure, advance to the next candidate, swap the
	// client config, and retry once. ModelConfig identifies the model the
	// failed attempt was served by (nil when unresolvable — see issue #30).
	c.resolver.RecordAliasFailure(c.aliasName, err, c.modelConfig)
	nextCfg, rerr := c.resolver.ResolveForAlias(c.aliasName, "")
	if rerr != nil || nextCfg == nil {
		return nil, fmt.Errorf("llm classification: %w (no alternate candidate: %v)", err, rerr)
	}
	c.logger.Warn("LLM classifier rotating to next alias candidate",
		"alias", c.aliasName,
		"model", nextCfg.ProviderID+"/"+nextCfg.ModelID,
		"error", err,
	)
	c.client.Reconfigure(nextCfg)
	c.modelConfig = nextCfg
	// Provenance tracking (leaf 01 of classifier-observability): from here
	// on, ResolvedModel() must report the rotated candidate — the model
	// that actually serves the classification.
	c.servedModel = resolvedModelID(nextCfg)

	resp, err = attempt()
	if err == nil {
		// Post-rotation success: modelConfig was swapped to the serving
		// candidate above, so the clear is identity-attributed (bughunt
		// 2026-09-08 item 14). Nil modelConfig degrades to alias-wide.
		c.resolver.RecordAliasSuccessModel(c.aliasName, c.modelConfig)
	}
	return resp, err
}

// ClassifyMulti detects multiple intents in a single input.
func (c *LLMClassifier) ClassifyMulti(ctx context.Context, input string, ctxMemory []memory.MemoryResult) []*Intent {
	if c.client == nil {
		return nil
	}

	// Check cooldown before attempting per-request classification.
	if c.unavailable.Load() {
		c.unavailMu.RLock()
		unavailUntil := c.unavailUntil
		c.unavailMu.RUnlock()
		if time.Now().Before(unavailUntil) {
			return nil
		}
		c.unavailable.Store(false)
	}

	// Use a prompt that asks LLM to detect ALL intents
	prompt := fmt.Sprintf(`Analyze this user request and identify ALL distinct intents.

A request may contain multiple independent tasks joined by "and", "also", "then", "but", "while", etc.

For EACH detected intent, output:
- intent: one of [%s]
- confidence: 0.0-1.0
- summary: brief description

User input: %s

Return ONLY valid JSON array: [{"intent": "debug", "confidence": 0.8, "summary": "..."}]

If only one intent is present, return a single-element array.
If no intents detected, return empty array [].`, laneList(), input)

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You are a multi-intent detector for an AI agent system. Identify ALL distinct intents in user requests."},
		{Role: llm.RoleUser, Content: prompt},
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.client.Chat(timeoutCtx, messages,
		llm.WithMaxTokens(effectiveClassificationCap(c.modelConfig)),
		llm.WithTemperature(0.1),
	)
	if err != nil {
		c.unavailable.Store(true)
		c.unavailMu.Lock()
		c.unavailUntil = time.Now().Add(c.cooldown)
		c.unavailMu.Unlock()
		c.logger.Debug("LLM multi-intent classification failed", "error", err)
		return nil
	}

	if resp == nil || resp.Content == "" {
		c.unavailable.Store(true)
		c.unavailMu.Lock()
		c.unavailUntil = time.Now().Add(c.cooldown)
		c.unavailMu.Unlock()
		return nil
	}

	// Parse the JSON array response
	jsonStr := extractJSONFromLLM(resp.Content)
	if jsonStr == "" || jsonStr[0] != '[' {
		return nil
	}

	var multiResp []struct {
		Intent     string  `json:"intent"`
		Confidence float64 `json:"confidence"`
		Summary    string  `json:"summary"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &multiResp); err != nil {
		c.logger.Debug("Failed to parse multi-intent response", "error", err)
		return nil
	}

	var intents []*Intent
	for _, r := range multiResp {
		intent := strings.ToLower(strings.TrimSpace(r.Intent))
		if !isValidIntent(intent) {
			continue
		}
		agentType := agentForIntent(intent)
		// Derive the flag from the lane's own rule, never a plan-only
		// equality. The handler's async gate (handler.go:770 ->
		// ShouldDispatchAsync -> Intent.RequiresPlanning) reads this
		// field, and every other producer (fallback, semantic, keyword)
		// sets it from IntentType.RequiresPlanning(). When only plan set
		// it, an LLM-classified quickplan carried false, the gate stayed
		// shut, and the lane 691d83f6 made emittable dead-ended
		// (bughunt 2026-09-12 C-0).
		requiresPlanning := IntentType(intent).RequiresPlanning()
		intents = append(intents, &Intent{
			Type:             intent,
			Confidence:       clampConfidence(r.Confidence),
			AgentType:        agentType,
			RequiresPlanning: requiresPlanning,
			Summary:          extractSummary(input),
		})
	}

	return intents
}

// MarkUnavailable explicitly sets the classifier as unavailable, preventing
// further classification attempts until the cooldown expires.
func (c *LLMClassifier) MarkUnavailable() {
	c.unavailable.Store(true)
	c.unavailMu.Lock()
	c.unavailUntil = time.Now().Add(c.cooldown)
	c.unavailMu.Unlock()
}

// UnmarkUnavailable clears the unavailable flag, allowing immediate retries.
func (c *LLMClassifier) UnmarkUnavailable() {
	c.unavailable.Store(false)
}

func (c *LLMClassifier) buildClassificationPrompt(input string) string {
	var sb strings.Builder
	sb.WriteString("Classify this user input:\n\n")
	sb.WriteString("Input: ")
	sb.WriteString(input)
	sb.WriteString("\n\n")
	sb.WriteString("Available intents:\n")
	for _, lane := range classifierLanes {
		intent := string(lane)
		sb.WriteString("- ")
		sb.WriteString(intent)
		sb.WriteString(": ")
		sb.WriteString(c.getIntentDescription(intent))
		sb.WriteString("\n")
	}
	sb.WriteString("\nReturn JSON with intent, confidence, and reasoning.")
	return sb.String()
}

func (c *LLMClassifier) getIntentDescription(intent string) string {
	descriptions := map[string]string{
		string(IntentGit):      "Git operations (commit, push, pull, merge, branch)",
		string(IntentSchedule): "Scheduling, reminders, timers, future tasks",
		// "create a file named X …" (e2e 2026-09-18/19) is CODE, not chat: the
		// description must name concrete artifact creation so the 8B stops
		// wandering to the catch-all lane. The "then tell me Y" tail is a
		// readback of the work's output, still code (report-readback collapse
		// backs this up at the dispatcher).
		string(IntentCode):      "Code writing, implementation, refactoring, and creating concrete artifacts: create/write a file, script, function, component, config, or document; includes an action plus a follow-up report of its result",
		string(IntentDebug):     "Bug fixing, debugging, error handling",
		string(IntentReview):    "Code review, PR review, assessing existing work",
		string(IntentPlan):      "Planning, architecture, design",
		string(IntentQuickPlan): "Do the work now, end to end, without check-ins: multi-step work executed immediately - e.g. review the code for bugs and correct them as you find them, implement the plan using subagents, implement tasks from a plan in order",
		// platform is QUESTIONS ABOUT the platform, not the act of using a
		// tool. The old wording ("Questions about agent capabilities, tools")
		// matched "call the <tool> tool" prompts and sent them here
		// (2026-09-12: an explicit json_extract request classified as
		// platform at 0.95).
		string(IntentPlatform):    "Questions about the platform itself: which agents exist, what the system can do, how it works",
		string(IntentToolUse):     "Run one specific named tool now with explicit arguments",
		string(IntentReport):      "Status reports, summaries of work done",
		string(IntentRecall):      "Memory recall, remembering past conversations",
		string(IntentAnalyze):     "Analysis, explanations, tradeoffs, comparisons",
		string(IntentSearch):      "Web search, finding information",
		string(IntentResearch):    "Research a topic: gather sources and extract structured data from them",
		string(IntentExplore):     "Read-only exploration of an existing codebase",
		string(IntentSecurity):    "Security review, secrets, permissions, vulnerability work",
		string(IntentStatus):      "Status inquiries about running work",
		string(IntentWrite):       "Writing prose: docs, README, release notes",
		string(IntentArchitect):   "System architecture decisions and designs",
		string(IntentSkeptic):     "Adversarial review: find what is wrong with a claim or design",
		string(IntentLibrarian):   "Organize or curate knowledge and references",
		string(IntentImageGen):    "Generate an image",
		string(IntentVideoGen):    "Generate a video",
		string(IntentImageID):     "Identify or describe the contents of an image",
		string(IntentInstruction): "Standing instructions: 'always', 'every day at', 'remember to'",
		string(IntentClarify):     "The request cannot be acted on until the user answers a question",
		string(IntentChat):        "General conversation, greetings, help",
	}
	if desc, ok := descriptions[intent]; ok {
		return desc
	}
	return "Unknown intent"
}

func (c *LLMClassifier) parseResponse(content, originalInput string) (*Intent, error) {
	var resp classificationResponse

	cleanContent := strings.TrimSpace(content)

	jsonStr := extractJSONFromLLM(cleanContent)
	if jsonStr != "" {
		if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
			return nil, fmt.Errorf("failed to parse classification response: %w", err)
		}
	}

	resp.Intent = strings.ToLower(strings.TrimSpace(resp.Intent))
	if resp.Intent == "" {
		return nil, fmt.Errorf("LLM classification: no intent returned")
	}

	if !isValidIntent(resp.Intent) {
		if c.logger != nil {
			c.logger.Debug("Invalid intent from LLM", "intent", resp.Intent)
		}
		return nil, fmt.Errorf("invalid intent: %s", resp.Intent)
	}

	agentType := agentForIntent(resp.Intent)

	// Same lane-derived rule as ClassifyMulti (bughunt 2026-09-12 C-0):
	// the async gate reads RequiresPlanning, so an LLM-emitted quickplan
	// must carry the value IntentType.RequiresPlanning() gives it or the
	// quickplan -> orchestrator pipeline is dead code for this producer.
	requiresPlanning := IntentType(resp.Intent).RequiresPlanning()

	return &Intent{
		Type:             resp.Intent,
		Confidence:       clampConfidence(resp.Confidence),
		AgentType:        agentType,
		RequiresPlanning: requiresPlanning,
		Summary:          extractSummary(originalInput),
	}, nil
}

func extractJSONFromLLM(s string) string {
	// Find the start of the first JSON object or array.
	start := -1
	for i := range s {
		if s[i] == '{' || s[i] == '[' {
			start = i
			break
		}
	}
	if start == -1 {
		return ""
	}

	// Use bracket counting to find the matching close bracket,
	// properly tracking string literals and escape sequences.
	depth := 0
	inString := false
	escape := false
	closeChar := byte('}')
	if s[start] == '[' {
		closeChar = ']'
	}

	for i := start; i < len(s); i++ {
		ch := s[i]

		if escape {
			escape = false
			continue
		}

		if ch == '\\' && inString {
			escape = true
			continue
		}

		if ch == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if ch == '{' || ch == '[' {
			depth++
		} else if ch == '}' || ch == ']' {
			depth--
			if depth == 0 && ch == closeChar {
				return s[start : i+1]
			}
		}
	}

	// No matching close bracket found
	return ""
}

func isValidIntent(intent string) bool {
	return classifierLaneSet[intent]
}

func clampConfidence(conf float64) float64 {
	if conf < 0 {
		return 0
	}
	if conf > 1 {
		return 1
	}
	return conf
}

func GetThresholdForIntent(intentType string) float64 {
	if threshold, ok := intentThresholds[intentType]; ok {
		return threshold
	}
	return 0.5
}

func ShouldUseLLMResult(intent *Intent) bool {
	if intent == nil {
		return false
	}
	threshold := GetThresholdForIntent(intent.Type)
	return intent.Confidence >= threshold
}
