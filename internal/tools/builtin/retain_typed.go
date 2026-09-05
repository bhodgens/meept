package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// asStringArg coerces a map[string]any argument value to a string. Returns
// empty string for non-strings.
func asStringArg(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// toStringSlice coerces a map[string]any argument value to a []string.
// Accepts []any and []string; returns nil for other types.
func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// parseRFC3339Arg reads an optional RFC3339 timestamp argument. Absent or
// empty values return (zero, false, nil). Non-string or unparseable values
// return an error — invalid input is never silently dropped.
func parseRFC3339Arg(args map[string]any, name string) (time.Time, bool, error) {
	raw, ok := args[name]
	if !ok || raw == "" {
		return time.Time{}, false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return time.Time{}, false, fmt.Errorf("invalid %s (want RFC3339 string)", name)
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("invalid %s (want RFC3339): %w", name, err)
	}
	return parsed.UTC(), true, nil
}

// RetainClaimTool is the Path A tool for storing user-asserted claims
// (ClaimStatusConfirmed by default). It wraps Manager.StoreClaim.
type RetainClaimTool struct {
	tools.ToolDefaults
	manager *memory.Manager
}

// NewRetainClaimTool constructs a RetainClaimTool bound to the given manager.
func NewRetainClaimTool(manager *memory.Manager) *RetainClaimTool {
	return &RetainClaimTool{manager: manager}
}

func (t *RetainClaimTool) Name() string     { return "retain_claim" }
func (t *RetainClaimTool) Category() string { return "memory" }

func (t *RetainClaimTool) Description() string {
	return "Record an explicit claim that the user is asserting as true. " +
		"Use this when the user states a belief, opinion, or fact they endorse. " +
		"Claims stored here carry full trust weight (confirmed)."
}

func (t *RetainClaimTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"text": {
				Type:        schemaTypeString,
				Description: "The claim itself, as a self-contained assertion.",
			},
			"confidence": {
				Type:        schemaTypeNumber,
				Description: "User-asserted confidence in the claim (0.0-1.0). Optional.",
			},
			"source": {
				Type:        schemaTypeString,
				Description: "Source citation: URL, paper, or 'user'. Optional.",
			},
			"premises": {
				Type:        schemaTypeArray,
				Description: "Supporting premise strings or claim IDs. Optional.",
				Items:       &llm.ParameterProperty{Type: schemaTypeString},
			},
			"tags": {
				Type:        schemaTypeArray,
				Description: "Controlled-vocabulary tags (see epistemic_tags.json5). Optional.",
				Items:       &llm.ParameterProperty{Type: schemaTypeString},
			},
			"valid_from": {
				Type:        schemaTypeString,
				Description: "RFC3339 timestamp: earliest instant the claim is in force. Optional; absent = unbounded.",
			},
			"valid_to": {
				Type:        schemaTypeString,
				Description: "RFC3339 timestamp: latest instant the claim is in force. Optional; absent = unbounded.",
			},
			"observed_at": {
				Type:        schemaTypeString,
				Description: "RFC3339 timestamp: when the claim was observed true. Optional; absent = store time.",
			},
		},
		Required: []string{"text"},
	}
}

func (t *RetainClaimTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	text := asStringArg(args["text"])
	if text == "" {
		return nil, fmt.Errorf("text is required")
	}

	var confidence float64
	if c, ok := args["confidence"].(float64); ok {
		confidence = c
	}
	claim := memory.Claim{
		Text:       text,
		Confidence: confidence,
		Source:     asStringArg(args["source"]),
		Premises:   toStringSlice(args["premises"]),
		Tags:       toStringSlice(args["tags"]),
		Status:     memory.ClaimStatusConfirmed,
	}
	if parsed, ok, err := parseRFC3339Arg(args, "valid_from"); err != nil {
		return nil, err
	} else if ok {
		claim.ValidFrom = &parsed
	}
	if parsed, ok, err := parseRFC3339Arg(args, "valid_to"); err != nil {
		return nil, err
	} else if ok {
		claim.ValidTo = &parsed
	}
	if parsed, ok, err := parseRFC3339Arg(args, "observed_at"); err != nil {
		return nil, err
	} else if ok {
		claim.ObservedAt = parsed
	}
	id, err := t.manager.StoreClaim(ctx, claim)
	if err != nil {
		return nil, fmt.Errorf("store claim: %w", err)
	}
	return map[string]any{
		"success":   true,
		"memory_id": id,
		"status":    string(memory.ClaimStatusConfirmed),
	}, nil
}

// ListExpiredClaimsTool surfaces claims whose validity window has closed.
// Hard-exclusion removes them from trust-weighted results silently; this
// tool is the visibility half of that contract.
type ListExpiredClaimsTool struct {
	tools.ToolDefaults
	manager *memory.Manager
}

// NewListExpiredClaimsTool constructs the tool bound to the given manager.
func NewListExpiredClaimsTool(manager *memory.Manager) *ListExpiredClaimsTool {
	return &ListExpiredClaimsTool{manager: manager}
}

func (t *ListExpiredClaimsTool) Name() string     { return "list_expired_claims" }
func (t *ListExpiredClaimsTool) Category() string { return "memory" }

func (t *ListExpiredClaimsTool) Description() string {
	return "List claims whose validity window (valid_to) has passed. " +
		"Expired claims are excluded from trust-weighted results; use this " +
		"to see what has aged out and decide on re-verification or rejection."
}

func (t *ListExpiredClaimsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"limit": {
				Type:        schemaTypeInteger,
				Description: "Max claims to return (default 20).",
			},
		},
	}
}

func (t *ListExpiredClaimsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	results, err := t.manager.ListExpiredClaims(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired claims: %w", err)
	}
	claims := make([]map[string]any, 0, len(results))
	for _, r := range results {
		vt, _ := r.Memory.Metadata["valid_to"].(string) // two-value: absent = ""
		rev := int64(0)
		if f, ok := r.Memory.Metadata["rev"].(float64); ok {
			rev = int64(f)
		}
		claims = append(claims, map[string]any{
			"memory_id": r.Memory.ID,
			"text":      r.Memory.Content,
			"valid_to":  vt,
			"rev":       rev,
			"status":    r.Memory.Metadata["status"],
			"reason":    "expired",
		})
	}
	return map[string]any{"success": true, "claims": claims}, nil
}

// RetainDecisionTool is the Path A tool for storing decisions with expected
// outcomes and optional review schedules. It wraps Manager.StoreDecision.
type RetainDecisionTool struct {
	tools.ToolDefaults
	manager *memory.Manager
}

// NewRetainDecisionTool constructs a RetainDecisionTool bound to the manager.
func NewRetainDecisionTool(manager *memory.Manager) *RetainDecisionTool {
	return &RetainDecisionTool{manager: manager}
}

func (t *RetainDecisionTool) Name() string     { return "retain_decision" }
func (t *RetainDecisionTool) Category() string { return "memory" }

func (t *RetainDecisionTool) Description() string {
	return "Record a decision the user has made along with the expected outcome " +
		"and optional review schedule. Use this when the user commits to a choice " +
		"and wants to track whether the expected outcome materialises."
}

func (t *RetainDecisionTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"call": {
				Type:        schemaTypeString,
				Description: "The decision made (e.g., 'adopt library X for parsing').",
			},
			"expected_outcome": {
				Type:        schemaTypeString,
				Description: "What the user expects to happen as a result.",
			},
			"alternatives": {
				Type:        schemaTypeArray,
				Description: "Alternatives that were considered and rejected. Optional.",
				Items:       &llm.ParameterProperty{Type: schemaTypeString},
			},
			"review_at": {
				Type:        schemaTypeString,
				Description: "RFC3339 timestamp when the decision should be revisited. Optional.",
			},
			"premises": {
				Type:        schemaTypeArray,
				Description: "Claim IDs this decision rests on. Optional.",
				Items:       &llm.ParameterProperty{Type: schemaTypeString},
			},
		},
		Required: []string{"call", "expected_outcome"},
	}
}

func (t *RetainDecisionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	call := asStringArg(args["call"])
	if call == "" {
		return nil, fmt.Errorf("call is required")
	}
	expectedOutcome := asStringArg(args["expected_outcome"])
	if expectedOutcome == "" {
		return nil, fmt.Errorf("expected_outcome is required")
	}

	decision := memory.Decision{
		Call:            call,
		ExpectedOutcome: expectedOutcome,
		Alternatives:    toStringSlice(args["alternatives"]),
		Premises:        toStringSlice(args["premises"]),
	}
	if reviewAtStr := asStringArg(args["review_at"]); reviewAtStr != "" {
		if parsed, err := time.Parse(time.RFC3339, reviewAtStr); err == nil {
			decision.ReviewAt = &parsed
		} else {
			return nil, fmt.Errorf("invalid review_at (use RFC3339): %w", err)
		}
	}

	id, err := t.manager.StoreDecision(ctx, decision)
	if err != nil {
		return nil, fmt.Errorf("store decision: %w", err)
	}
	return map[string]any{
		"success":   true,
		"memory_id": id,
		"status":    "open",
	}, nil
}

// RetainPredictionTool is the Path A tool for storing forecasts with a
// resolution horizon. It wraps Manager.StorePrediction.
type RetainPredictionTool struct {
	tools.ToolDefaults
	manager *memory.Manager
}

// NewRetainPredictionTool constructs a RetainPredictionTool bound to the manager.
func NewRetainPredictionTool(manager *memory.Manager) *RetainPredictionTool {
	return &RetainPredictionTool{manager: manager}
}

func (t *RetainPredictionTool) Name() string     { return "retain_prediction" }
func (t *RetainPredictionTool) Category() string { return "memory" }

func (t *RetainPredictionTool) Description() string {
	return "Record a prediction or forecast with a resolution horizon. " +
		"Predictions are tracked for later review: when the horizon passes, " +
		"the prediction is surfaced for resolution (mark_resolved)."
}

func (t *RetainPredictionTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"forecast": {
				Type:        schemaTypeString,
				Description: "The prediction itself.",
			},
			"horizon": {
				Type:        schemaTypeString,
				Description: "RFC3339 timestamp when the prediction should resolve.",
			},
			"related_decision": {
				Type:        schemaTypeString,
				Description: "Decision memory ID this prediction stems from. Optional.",
			},
		},
		Required: []string{"forecast", "horizon"},
	}
}

func (t *RetainPredictionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}
	forecast := asStringArg(args["forecast"])
	if forecast == "" {
		return nil, fmt.Errorf("forecast is required")
	}
	horizonStr := asStringArg(args["horizon"])
	if horizonStr == "" {
		return nil, fmt.Errorf("horizon is required")
	}
	horizon, err := time.Parse(time.RFC3339, horizonStr)
	if err != nil {
		return nil, fmt.Errorf("invalid horizon (use RFC3339): %w", err)
	}

	prediction := memory.Prediction{
		Forecast:        forecast,
		Horizon:         horizon,
		RelatedDecision: asStringArg(args["related_decision"]),
	}
	id, err := t.manager.StorePrediction(ctx, prediction)
	if err != nil {
		return nil, fmt.Errorf("store prediction: %w", err)
	}
	return map[string]any{
		"success":   true,
		"memory_id": id,
		"status":    "open",
	}, nil
}

// Ensure Path A tools satisfy the Tool interface.
var (
	_ tools.Tool = (*RetainClaimTool)(nil)
	_ tools.Tool = (*ListExpiredClaimsTool)(nil)
	_ tools.Tool = (*RetainDecisionTool)(nil)
	_ tools.Tool = (*RetainPredictionTool)(nil)
)
