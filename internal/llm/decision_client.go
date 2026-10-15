package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// DecisionClient: typed-decision seam (decision-model tree leaf 02,
// docs/plans/decision-model/02-decision-client.md, Contract B in master.md).
//
// The client abstracts "ask ONE typed question against the decision model".
// Callers treat ErrDecisionUnavailable as "feature disabled", never a turn
// failure — every decision-model consumer path must work unchanged when the
// slot is empty. The serving shape behind Ask is the prompt-parse fallback
// from master.md: the rendered question goes out as a ONE-token, temperature-0
// completion through the same client-resolution path the classifier uses
// (resolver model config -> Chatter via createChatterFor, the OpenAI /
// Anthropic / Codex transport selector at provider_manager.go). When the
// provider surfaces label-token logprobs the Raw map carries the calibrated
// distribution; otherwise argmax-only is returned with Confidence 1.0 and the
// degradation is documented inline below.

var (
	// ErrDecisionUnavailable means no decision model is configured
	// (decision_model slot empty or unresolvable). Callers must treat it as
	// "feature disabled", never a turn failure.
	ErrDecisionUnavailable = errors.New("decision model not configured")

	// ErrDecisionParse means the model's answer token did not exactly match
	// any of the question's option labels (after trim + case folding).
	ErrDecisionParse = errors.New("decision answer does not match any option")
)

// DecisionQuestion is one typed question for the decision model.
type DecisionQuestion struct {
	// State is the serialized context the question is asked against
	// (rendered state).
	State string
	// Type is the question kind: "choice" | "score" | "noul".
	Type string
	// Prompt is the question text.
	Prompt string
	// Options are the choice labels; each must be ONE token after render.
	Options []string
	// Threshold is the escalate-above bar, 0-1 (Contract B).
	Threshold float64
}

// DecisionResult is the parsed answer for one DecisionQuestion.
type DecisionResult struct {
	// Answer is the canonical option label that was chosen.
	Answer string
	// Confidence is the calibrated 0-1 probability of the answer — a
	// calibration signal, NOT accuracy. When the provider returns no
	// logprobs, this degrades to 1.0 (argmax-only; see Ask).
	Confidence float64
	// Raw is the label→probability distribution. Full when the provider
	// returns logprobs over the option tokens; argmax-only otherwise.
	Raw map[string]float64
}

// DecisionClient asks typed questions against the configured decision model.
// It is resolver-backed: construction takes the Resolver (leaf 01 owns the
// decisionModel slot) and resolves the serving Chatter through the SAME path
// the classifier's endpoint uses — resolver model config -> createChatterFor
// — never a new transport.
type DecisionClient struct {
	resolver *Resolver
	logger   *slog.Logger
	chatter  Chatter // resolved lazily; injectable for tests

	// chatterOnce guards the lazy chatter resolution.
	chatterOnce bool
}

// NewDecisionClient builds a DecisionClient over the Resolver. When no
// decision model is configured (DecisionModel() == nil), Available() is false
// and every Ask returns ErrDecisionUnavailable.
func NewDecisionClient(r *Resolver, logger *slog.Logger) *DecisionClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &DecisionClient{resolver: r, logger: logger}
}

// Available reports whether a decision model is configured. False means every
// decision-model feature stays disabled (leaf-01 semantics: the slot never
// falls back to small_model or the default model).
func (d *DecisionClient) Available() bool {
	return d != nil && d.resolver != nil && d.resolver.DecisionModel() != nil
}

// ModelName returns "provider/model-id" of the decision model, or "" when
// unconfigured.
func (d *DecisionClient) ModelName() string {
	if !d.Available() {
		return ""
	}
	return d.resolver.DecisionModelName()
}

// chatterFor resolves the serving Chatter for the decision model through the
// classifier's resolution path: the resolver's decision-model ModelConfig is
// handed to createChatterFor (the same OpenAI/Anthropic/Codex transport
// selector ProviderManager and ModelBroker use for every serving client).
// The Chatter interface is satisfied by the result, so tests can inject a
// stub instead.
func (d *DecisionClient) chatterFor() (Chatter, error) {
	if d.chatter != nil {
		return d.chatter, nil
	}
	if !d.Available() {
		return nil, ErrDecisionUnavailable
	}
	if !d.chatterOnce {
		cfg := d.resolver.DecisionModel()
		d.chatter = createChatterFor(cfg, nil, d.logger, nil)
		d.chatterOnce = true
	}
	if d.chatter == nil {
		return nil, ErrDecisionUnavailable
	}
	return d.chatter, nil
}

// Ask asks ONE typed question against the decision model and parses the
// single-token answer back to an option label.
//
// Serving shape: State + Prompt + numbered Options are rendered into one chat
// prompt and sent as a max_tokens=1, temperature-0 completion. The answer is
// parsed trim + case-insensitive, exact match against the option labels; any
// mismatch is ErrDecisionParse. Raw map: label logprobs when the provider
// returns them; otherwise argmax-only with Confidence 1.0 — the documented
// degradation (no calibrated distribution available without logprobs).
func (d *DecisionClient) Ask(ctx context.Context, q DecisionQuestion) (*DecisionResult, error) {
	if !d.Available() {
		return nil, ErrDecisionUnavailable
	}
	if len(q.Options) == 0 {
		return nil, fmt.Errorf("decision question %q: no options", q.Type)
	}

	chatter, err := d.chatterFor()
	if err != nil {
		return nil, err
	}

	messages := []ChatMessage{
		{Role: RoleSystem, Content: "You are a decision endpoint. Answer with EXACTLY one option label and nothing else."},
		{Role: RoleUser, Content: renderDecisionPrompt(q)},
	}

	// ONE token, temperature 0: the decision endpoint contract is a typed
	// answer, zero generated tokens beyond the label.
	resp, err := chatter.Chat(ctx, messages,
		WithMaxTokens(1),
		WithTemperature(0),
	)
	if err != nil {
		return nil, fmt.Errorf("decision model call failed: %w", err)
	}

	answer, err := parseDecisionAnswer(resp.Content, q.Options)
	if err != nil {
		return nil, err
	}

	// Raw map degradation: no label-token logprobs are exposed through the
	// Chatter Response shape, so the argmax-only distribution is returned
	// with Confidence 1.0. Downstream consumers must NOT read Confidence=1.0
	// as a calibrated certainty — it is the documented argmax-only floor.
	// TODO(decision-model leaf 03/04): when a serving stack exposes label
	// logprobs (simple-jev pattern), populate Raw with the real distribution.
	raw := make(map[string]float64, len(q.Options))
	for _, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt), answer) {
			raw[opt] = 1.0
		}
	}

	return &DecisionResult{
		Answer:     answer,
		Confidence: 1.0,
		Raw:        raw,
	}, nil
}

// renderDecisionPrompt renders State + Prompt + numbered Options into the
// single user prompt sent to the decision model.
func renderDecisionPrompt(q DecisionQuestion) string {
	var b strings.Builder
	if q.State != "" {
		b.WriteString("Context:\n")
		b.WriteString(q.State)
		b.WriteString("\n\n")
	}
	b.WriteString(q.Prompt)
	b.WriteString("\n\nOptions:\n")
	for i, opt := range q.Options {
		fmt.Fprintf(&b, "%d. %s\n", i+1, opt)
	}
	b.WriteString("\nAnswer with exactly one option label.")
	return b.String()
}

// parseDecisionAnswer maps the model's answer text to a canonical option
// label: trim, case-insensitive, exact match. Any other answer (empty,
// whitespace, unknown label) is ErrDecisionParse.
func parseDecisionAnswer(content string, options []string) (string, error) {
	answer := strings.ToLower(strings.TrimSpace(content))
	if answer == "" {
		return "", fmt.Errorf("%w: empty answer", ErrDecisionParse)
	}
	for _, opt := range options {
		if strings.ToLower(strings.TrimSpace(opt)) == answer {
			return opt, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not one of %v", ErrDecisionParse, content, options)
}
