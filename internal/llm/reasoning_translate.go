package llm

import (
	"log/slog"
	"strings"
)

// shouldSendReasoning decides whether reasoning/thinking fields should be
// attached to an outgoing request for the given model. It returns false when
// the config is zero-valued, true when Force is set (with a debug log), and
// otherwise consults the model's capability tags.
func shouldSendReasoning(cfg *ModelConfig, rc *ReasoningConfig) bool {
	if rc.IsZero() {
		return false
	}
	if rc.Force {
		slog.Debug("reasoning force=true, bypassing capability gate",
			"model", cfg.ModelID,
			"provider", cfg.ProviderID,
		)
		return true
	}
	return cfg.HasCapability(CapReasoning) || cfg.HasCapability(CapThinking) || cfg.HasCapability("extended_thinking")
}

// applyOpenAICompatReasoning mutates a chat-completion request body in place
// to add vendor-specific reasoning fields. The globalBudgets map may be nil;
// ResolveBudget falls back to the hardcoded default table when it is.
//
// The function is a no-op when rc is zero-valued or the model lacks
// reasoning capability (unless rc.Force is set).
func applyOpenAICompatReasoning(body map[string]any, cfg *ModelConfig, rc *ReasoningConfig, globalBudgets map[string]int) {
	if rc.IsZero() || !shouldSendReasoning(cfg, rc) {
		return
	}

	effort := rc.Effort

	switch cfg.ProviderID {
	case ProviderIDOpenAI, "xai", "grok":
		// xAI/Grok support only low/high — clamp the intermediate tiers.
		effective := effort
		if cfg.ProviderID == "xai" || cfg.ProviderID == "grok" {
			effective = clampXAIEffort(effort)
		}
		if effective != "" && effective != ReasoningNone {
			body["reasoning_effort"] = effective
		}

	case ProviderIDGoogle, "google-oauth":
		// Gemini accepts reasoning_effort via extra_body.
		if effort != "" && effort != ReasoningNone {
			extra, _ := body["extra_body"].(map[string]any)
			if extra == nil {
				extra = make(map[string]any)
			}
			extra["reasoning_effort"] = effort
			body["extra_body"] = extra
		}

	case ProviderIDZAI, "moonshot":
		// GLM and Kimi use the Anthropic-style thinking block.
		applyZAIStyleThinking(body, rc, cfg, globalBudgets)

	case ProviderIDOllama, "qwen":
		// Qwen3 / Qwq: boolean enable_thinking + thinking_budget.
		enable := rc.ResolveEnabled()
		body["enable_thinking"] = enable
		if budget := ResolveBudget(rc, nil, nil, globalBudgets); budget != nil {
			body["thinking_budget"] = *budget
		}

	case ProviderIDDeepSeek:
		// DeepSeek does reasoning by default; no request field needed.
		// `reasoning_content` surfaces in responses and is parsed elsewhere.

	case "openrouter":
		// Dual-send: OpenRouter's meta-field + native upstream field.
		if effort != "" && effort != ReasoningNone {
			body["reasoning"] = map[string]any{"effort": effort}
		}
		// When the upstream is Anthropic, also send the native thinking block.
		if strings.HasPrefix(strings.ToLower(cfg.ModelID), "anthropic/") {
			applyZAIStyleThinking(body, rc, cfg, globalBudgets)
		}

	default:
		// local, together, groq, github-models, etc.: passthrough.
		// No vendor-specific field sent.
	}
}

// applyZAIStyleThinking writes the `thinking: {type, budget_tokens}` field
// used by GLM (zai), Kimi (moonshot), and OpenRouter's Anthropic passthrough.
func applyZAIStyleThinking(body map[string]any, rc *ReasoningConfig, cfg *ModelConfig, globalBudgets map[string]int) {
	effort := rc.Effort

	// Explicit disable.
	if effort == ReasoningNone {
		body["thinking"] = map[string]any{"type": "disabled"}
		return
	}

	thinking := map[string]any{"type": "enabled"}
	if budget := ResolveBudget(rc, nil, cfg.DefaultReasoning, globalBudgets); budget != nil {
		thinking["budget_tokens"] = *budget
	}
	body["thinking"] = thinking
}

// clampXAIEffort clamps an effort tier to the values xAI/Grok accept
// (low/high only). medium→low, xhigh/max→high. Empty and none pass through.
func clampXAIEffort(effort string) string {
	switch effort {
	case ReasoningMedium, ReasoningLow:
		if effort == ReasoningMedium {
			slog.Debug("reasoning effort clamped for xAI/grok",
				"original", effort,
				"clamped", ReasoningLow,
			)
			return ReasoningLow
		}
		return ReasoningLow
	case ReasoningXHigh, ReasoningMax, ReasoningHigh:
		if effort != ReasoningHigh {
			slog.Debug("reasoning effort clamped for xAI/grok",
				"original", effort,
				"clamped", ReasoningHigh,
			)
		}
		return ReasoningHigh
	default:
		return effort
	}
}

// applyAnthropicReasoning mutates an anthropicRequest in place to configure
// the thinking block according to rc. Callers should invoke this after
// building the base request.
//
// Behavior:
//   - When rc.IsZero() or the model lacks capability (and Force is unset),
//     the request is left untouched (legacy capability-driven default still
//     applies via buildRequest's own extended_thinking check).
//   - When effort=="none", req.Thinking is forced to nil (disable).
//   - When effort is any other non-empty tier, thinking is enabled with the
//     resolved budget.
//   - When effort=="" but rc.Force or CapThinking/CapReasoning/extended_thinking
//     is present, thinking is enabled without a budget (matches legacy wire
//     format for capability-only requests).
//nolint:unused -- reserved for future Anthropic vendor support
func applyAnthropicReasoning(req *anthropicRequest, cfg *ModelConfig, rc *ReasoningConfig, globalBudgets map[string]int) {
	if rc.IsZero() || !shouldSendReasoning(cfg, rc) {
		return
	}

	effort := rc.Effort

	// Explicit disable wins over everything (spec §10.1).
	if effort == ReasoningNone {
		req.Thinking = nil
		return
	}

	if effort != "" {
		thinking := &anthropicThinkingConfig{Type: "enabled"}
		if budget := ResolveBudget(rc, nil, cfg.DefaultReasoning, globalBudgets); budget != nil {
			b := *budget
			thinking.BudgetTokens = &b
		}
		req.Thinking = thinking
		return
	}

	// effort=="" but rc.Force or capability present — enable without budget.
	req.Thinking = &anthropicThinkingConfig{Type: "enabled"}
}
