package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Decision-model tree leaf 02 (docs/plans/decision-model/02-decision-client.md):
// pins the DecisionClient typed-decision seam — Available/Ask semantics, the
// ErrDecisionUnavailable sentinel (feature disabled, never a failure), answer
// parsing (trim + case-insensitive exact match), and ErrDecisionParse on
// mismatch. Chatter stub pattern reused from context_compressor_test.go
// (mockChatter).

// decisionStubChatter is a Chatter test double that returns a canned response
// and captures the messages it was called with.
type decisionStubChatter struct {
	response *Response
	err      error
	called   bool
	messages []ChatMessage
}

func (s *decisionStubChatter) Chat(_ context.Context, messages []ChatMessage, _ ...ChatOption) (*Response, error) {
	s.called = true
	s.messages = messages
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func (s *decisionStubChatter) ChatWithProgress(ctx context.Context, messages []ChatMessage, _ ProgressCallback, _ ...ChatOption) (*Response, error) {
	return s.Chat(ctx, messages)
}

func (s *decisionStubChatter) Config() *ModelConfig {
	return &ModelConfig{}
}

// decisionTestResolver builds a resolver with the decision slot configured,
// mirroring the leaf-01 test wiring.
func decisionTestResolver(t *testing.T) *Resolver {
	t.Helper()
	return NewResolver(&ProvidersConfig{
		Model:         "prov/main",
		DecisionModel: "prov/decision",
		Providers: map[string]ProviderConfig{
			"prov": {
				API: "openai",
				Models: map[string]ModelDef{
					"main":     {Name: "main"},
					"decision": {Name: "decision"},
				},
			},
		},
	}, nil)
}

func TestDecisionClient_Unconfigured(t *testing.T) {
	r := NewResolver(&ProvidersConfig{
		Model:     "prov/main",
		Providers: map[string]ProviderConfig{},
	}, nil)
	d := NewDecisionClient(r, nil)

	if d.Available() {
		t.Fatal("Available() = true with no decision_model slot, want false")
	}
	_, err := d.Ask(context.Background(), DecisionQuestion{
		Type:    "choice",
		Prompt:  "pick one",
		Options: []string{"yes", "no"},
	})
	if !errors.Is(err, ErrDecisionUnavailable) {
		t.Fatalf("Ask err = %v, want ErrDecisionUnavailable", err)
	}
}

func TestDecisionClient_UnresolvableRef(t *testing.T) {
	// Leaf-01 semantics: an unresolvable ref resolves to nil at the resolver,
	// so the client must report the feature as disabled, not panic.
	r := NewResolver(&ProvidersConfig{
		Model:         "prov/main",
		DecisionModel: "prov/does-not-exist",
		Providers: map[string]ProviderConfig{
			"prov": {API: "openai", Models: map[string]ModelDef{"main": {Name: "main"}}},
		},
	}, nil)
	d := NewDecisionClient(r, nil)
	if d.Available() {
		t.Fatal("Available() = true for unresolvable decision_model ref, want false")
	}
}

func TestDecisionClient_HappyPath(t *testing.T) {
	r := decisionTestResolver(t)
	d := NewDecisionClient(r, nil)
	if !d.Available() {
		t.Fatal("Available() = false with configured decision model, want true")
	}
	if got := d.ModelName(); got != "prov/decision" {
		t.Fatalf("ModelName() = %q, want %q", got, "prov/decision")
	}

	stub := &decisionStubChatter{response: &Response{Content: "  Yes  "}}
	d.chatter = stub

	res, err := d.Ask(context.Background(), DecisionQuestion{
		State:  "the user just asked to delete a file",
		Type:   "choice",
		Prompt: "May this action run?",
		Options: []string{
			"yes", "no",
		},
		Threshold: 0.7,
	})
	if err != nil {
		t.Fatalf("Ask err = %v, want nil", err)
	}
	if !stub.called {
		t.Fatal("stub chatter was not called")
	}
	if res.Answer != "yes" {
		t.Fatalf("Answer = %q, want %q (canonical option label)", res.Answer, "yes")
	}
	if res.Confidence != 1.0 {
		t.Fatalf("Confidence = %v, want 1.0 (argmax-only degradation)", res.Confidence)
	}
	if res.Raw["yes"] != 1.0 {
		t.Fatalf("Raw = %v, want map[yes:1.0] (argmax-only degradation)", res.Raw)
	}

	// Serving shape: State + Prompt + numbered Options rendered into ONE
	// user prompt.
	if len(stub.messages) == 0 {
		t.Fatal("no messages sent to chatter")
	}
	combined := ""
	for _, m := range stub.messages {
		combined += m.Content + "\n"
	}
	for _, want := range []string{"the user just asked to delete a file", "May this action run?", "1. yes", "2. no"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("rendered prompt missing %q; got:\n%s", want, combined)
		}
	}
}

func TestDecisionClient_ParseFailure(t *testing.T) {
	r := decisionTestResolver(t)
	d := NewDecisionClient(r, nil)

	t.Run("empty_answer", func(t *testing.T) {
		d.chatter = &decisionStubChatter{response: &Response{Content: "   "}}
		_, err := d.Ask(context.Background(), DecisionQuestion{
			Type:    "choice",
			Prompt:  "pick",
			Options: []string{"yes", "no"},
		})
		if !errors.Is(err, ErrDecisionParse) {
			t.Fatalf("empty answer err = %v, want ErrDecisionParse", err)
		}
	})

	t.Run("option_mismatch", func(t *testing.T) {
		d.chatter = &decisionStubChatter{response: &Response{Content: "maybe"}}
		_, err := d.Ask(context.Background(), DecisionQuestion{
			Type:    "choice",
			Prompt:  "pick",
			Options: []string{"yes", "no"},
		})
		if !errors.Is(err, ErrDecisionParse) {
			t.Fatalf("mismatched answer err = %v, want ErrDecisionParse", err)
		}
	})
}
