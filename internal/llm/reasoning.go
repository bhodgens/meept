package llm

import "fmt"

// Reasoning effort tier constants. The zero value (empty string) means "do not
// send any reasoning field" — the model uses its provider default.
const (
	ReasoningNone   = "none"   // explicitly disable thinking
	ReasoningLow    = "low"    // minimal thinking budget
	ReasoningMedium = "medium" // balanced thinking
	ReasoningHigh   = "high"   // deep thinking
	ReasoningXHigh  = "xhigh"  // extra-deep thinking
	ReasoningMax    = "max"    // maximum thinking budget
)

// effortOrder maps effort tier names to an integer rank used by ClampEffort.
// Both "" and "none" map to 0 so that "no override" and "explicitly disabled"
// sort identically.
var effortOrder = map[string]int{
	"":       0,
	"none":   0,
	"low":    1,
	"medium": 2,
	"high":   3,
	"xhigh":  4,
	"max":    5,
}

// defaultBudgetTable is the hardcoded fallback tier→token mapping used when
// neither the user's config nor any ReasoningConfig specifies a budget.
var defaultBudgetTable = map[string]int{
	"low":    2000,
	"medium": 8000,
	"high":   16000,
	"xhigh":  32000,
	"max":    64000,
}

// DefaultBudgetTable returns a copy of the hardcoded tier→budget defaults.
// Callers may mutate the returned map without affecting the package-level
// table.
func DefaultBudgetTable() map[string]int {
	out := make(map[string]int, len(defaultBudgetTable))
	for k, v := range defaultBudgetTable {
		out[k] = v
	}
	return out
}

// IsValidEffort reports whether s is a recognized effort tier (including the
// empty string and "none").
func IsValidEffort(s string) bool {
	_, ok := effortOrder[s]
	return ok
}

// ReasoningConfig captures LLM reasoning/thinking configuration across vendors.
// A nil pointer or zero-value struct means "do not send" — defer to provider
// default. Vendors translate this into their native wire format via
// applyOpenAICompatReasoning / applyAnthropicReasoning.
type ReasoningConfig struct {
	// Effort is the named tier. Empty = don't send (use provider default).
	// "none" = send explicit disable when the vendor supports it.
	Effort string `json:"effort,omitempty"`

	// BudgetTokens overrides tier→budget mapping. When non-nil, used as the
	// raw thinking budget for vendors that accept token counts (Anthropic,
	// GLM, Kimi, Qwen). Ignored by vendors that only accept named tiers
	// (OpenAI, xAI, Gemini-compat).
	BudgetTokens *int `json:"budget_tokens,omitempty"`

	// Enabled explicitly toggles thinking on/off for vendors with a boolean
	// toggle (Qwen enable_thinking, GLM thinking.enabled). When nil, derived
	// from Effort (nil or any tier other than "none" → true).
	Enabled *bool `json:"enabled,omitempty"`

	// Force bypasses capability gating. Use when a model supports thinking
	// but lacks the "reasoning"/"extended_thinking" capability tag. Logs a
	// warning when invoked.
	Force bool `json:"force,omitempty"`
}

// IsZero reports whether the config carries no meaningful fields. A nil
// pointer is considered zero.
func (r *ReasoningConfig) IsZero() bool {
	if r == nil {
		return true
	}
	return r.Effort == "" && r.BudgetTokens == nil && r.Enabled == nil && !r.Force
}

// ResolveEnabled returns the effective on/off state. When Enabled is nil it
// is derived from Effort (any non-empty, non-"none" tier → true).
func (r *ReasoningConfig) ResolveEnabled() bool {
	if r == nil {
		return false
	}
	if r.Enabled != nil {
		return *r.Enabled
	}
	return r.Effort != "" && r.Effort != ReasoningNone
}

// Validate returns an error if the config's fields conflict — currently the
// only check is Enabled=false while Effort is a non-none, non-empty tier.
func (r *ReasoningConfig) Validate() error {
	if r == nil {
		return nil
	}
	if r.Enabled != nil && !*r.Enabled {
		if r.Effort != "" && r.Effort != ReasoningNone {
			return fmt.Errorf("reasoning: Enabled=false conflicts with Effort=%q (set Effort to \"none\" or \"\" instead)", r.Effort)
		}
	}
	if r.Effort != "" && !IsValidEffort(r.Effort) {
		return fmt.Errorf("reasoning: invalid effort tier %q", r.Effort)
	}
	return nil
}

// AgentReasoningConfig is the per-agent config form. It adds admin-defined
// bounds for self-modulation. Convert to a per-request ReasoningConfig with
// ToReasoningConfig.
type AgentReasoningConfig struct {
	// Effort is the initial tier used at agent startup and as the fallback
	// when the loop hasn't self-modulated.
	Effort string `json:"effort,omitempty"`

	// AllowSelfModulation permits the agent loop to change effort between
	// turns. Default false.
	AllowSelfModulation bool `json:"allow_self_modulation,omitempty"`

	// MinEffort / MaxEffort bound self-modulation. Empty = no bound on
	// that side.
	MinEffort string `json:"min_effort,omitempty"`
	MaxEffort string `json:"max_effort,omitempty"`

	// BudgetTokens is passed through to ReasoningConfig.
	BudgetTokens *int `json:"budget_tokens,omitempty"`

	// Force bypasses capability gating (forwarded to ReasoningConfig).
	Force bool `json:"force,omitempty"`
}

// ToReasoningConfig converts the agent config into a request-level
// ReasoningConfig at the given effective tier. The resulting config carries
// the agent's BudgetTokens and Force values; Enabled is derived from the
// tier (any non-empty, non-"none" value → true).
func (a *AgentReasoningConfig) ToReasoningConfig(effort string) *ReasoningConfig {
	if a == nil {
		// Nil agent config still returns a usable ReasoningConfig so callers
		// can pass nil without special-casing. The result is zero-valued
		// when effort is empty.
		if effort == "" {
			return nil
		}
		return &ReasoningConfig{Effort: effort}
	}
	enabled := effort != "" && effort != ReasoningNone
	rc := &ReasoningConfig{
		Effort:       effort,
		BudgetTokens: a.BudgetTokens,
		Enabled:      &enabled,
		Force:        a.Force,
	}
	// When effort is empty the Enabled pointer is misleading; drop it so
	// IsZero can return true for a truly empty config.
	if effort == "" {
		rc.Enabled = nil
	}
	if rc.IsZero() {
		return nil
	}
	return rc
}

// ClampEffort returns effort bounded by [MinEffort, MaxEffort] using
// effortOrder for rank comparison. Empty bounds mean unbounded on that side.
// When both bounds are empty AND the requested effort is empty, the result
// defaults to ReasoningMedium (per spec: "treat ” requested as ReasoningMedium
// default for clamping when both bounds empty"). When only the requested
// effort is empty (but bounds are set), it defaults to MinEffort (or
// ReasoningMedium when MinEffort is empty).
func (a *AgentReasoningConfig) ClampEffort(effort string) string {
	if a == nil {
		return effort
	}
	hasMin := a.MinEffort != ""
	hasMax := a.MaxEffort != ""

	// Default the requested effort when empty.
	if effort == "" {
		if !hasMin && !hasMax {
			return ReasoningMedium
		}
		// Default to MinEffort when set, otherwise ReasoningMedium.
		if a.MinEffort != "" {
			effort = a.MinEffort
		} else {
			effort = ReasoningMedium
		}
	}

	rank, ok := effortOrder[effort]
	if !ok {
		// Unrecognized effort — leave unchanged.
		return effort
	}

	if hasMin {
		if minRank, ok := effortOrder[a.MinEffort]; ok && rank < minRank {
			return a.MinEffort
		}
	}
	if hasMax {
		if maxRank, ok := effortOrder[a.MaxEffort]; ok && rank > maxRank {
			return a.MaxEffort
		}
	}
	return effort
}

// ResolveReasoning walks the precedence chain and returns the effective
// ReasoningConfig for a single LLM call.
//
// Order (highest to lowest):
//  1. perRequest    — from CLI flag / HTTP body / RPC param / NL parse
//  2. agentSpec     — AgentReasoningConfig.Effort converted to ReasoningConfig
//  3. modelDefault  — ModelConfig.DefaultReasoning
//  4. nil           — defer to provider (current behavior)
func ResolveReasoning(perRequest, agentSpec, modelDefault *ReasoningConfig) *ReasoningConfig {
	if perRequest != nil && !perRequest.IsZero() {
		return perRequest
	}
	if agentSpec != nil && !agentSpec.IsZero() {
		return agentSpec
	}
	if modelDefault != nil && !modelDefault.IsZero() {
		return modelDefault
	}
	return nil
}

// ResolveBudget resolves the thinking token budget for a request.
//
// Precedence (highest to lowest):
//  1. perRequest.BudgetTokens
//  2. agent.BudgetTokens
//  3. modelDefault.BudgetTokens
//  4. globalBudgets[effort]
//  5. hardcoded default table (see defaultBudgetTable)
//
// Returns nil when rc is nil/zero so callers can omit the budget from wire
// payloads entirely.
func ResolveBudget(rc *ReasoningConfig, agent *AgentReasoningConfig, modelDefault *ReasoningConfig, globalBudgets map[string]int) *int {
	if rc == nil || rc.IsZero() {
		return nil
	}

	// 1. per-request
	if rc.BudgetTokens != nil {
		v := *rc.BudgetTokens
		return &v
	}

	// 2. agent
	if agent != nil && agent.BudgetTokens != nil {
		v := *agent.BudgetTokens
		return &v
	}

	// 3. model default
	if modelDefault != nil && modelDefault.BudgetTokens != nil {
		v := *modelDefault.BudgetTokens
		return &v
	}

	// 4. global budgets map
	effort := rc.Effort
	if effort != "" && effort != ReasoningNone {
		if globalBudgets != nil {
			if v, ok := globalBudgets[effort]; ok && v > 0 {
				return &v
			}
		}
		// 5. hardcoded default table
		if v, ok := defaultBudgetTable[effort]; ok && v > 0 {
			return &v
		}
	}

	return nil
}
