package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// jev-command-gate leaf 01 (docs/plans/20261031-jev-command-gate/
// 01-scorer-gate.md, Contract C1): ScoreCommand over the decision client.
// Stub chatter pattern reused from decision_client_test.go.

func TestScoreCommand_Unconfigured(t *testing.T) {
	r := NewResolver(&ProvidersConfig{
		Model:     "prov/main",
		Providers: map[string]ProviderConfig{},
	}, nil)
	d := NewDecisionClient(r, nil)

	_, err := d.ScoreCommand(context.Background(), "command: ls\ncwd: /tmp\nfacts:\n")
	if !errors.Is(err, ErrDecisionUnavailable) {
		t.Fatalf("ScoreCommand err = %v, want ErrDecisionUnavailable", err)
	}
}

func TestScoreCommand_BandAnswer(t *testing.T) {
	d := NewDecisionClient(decisionTestResolver(t), nil)
	stub := &decisionStubChatter{response: &Response{Content: " 0.8 "}}
	d.chatter = stub

	res, err := d.ScoreCommand(context.Background(), "command: npm install\ncwd: /repo\nfacts:\n")
	if err != nil {
		t.Fatalf("ScoreCommand err = %v", err)
	}
	if res.Score != 0.8 {
		t.Fatalf("Score = %v, want 0.8", res.Score)
	}
	if res.Confidence != 1.0 {
		t.Fatalf("Confidence = %v, want the argmax-only 1.0 floor", res.Confidence)
	}
	if !res.Degraded {
		t.Fatal("Degraded = false; the argmax-only serving stack degrades EVERY result (production-degraded shape)")
	}
	if res.Answer != "0.8" {
		t.Fatalf("Answer = %q, want the trimmed band label", res.Answer)
	}

	// The fixed rubric question rides the decision question shape: the
	// prompt carries the band rubric, options are the fixed bands.
	if stub.messages == nil || len(stub.messages) != 2 {
		t.Fatalf("messages = %+v, want system+user pair", stub.messages)
	}
	user := stub.messages[1].Content
	for _, want := range []string{"0.0 - purely read-only", "1.0 - catastrophic", "command: npm install"} {
		if !strings.Contains(user, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestScoreCommand_NonBandAnswerIsParseError(t *testing.T) {
	d := NewDecisionClient(decisionTestResolver(t), nil)
	d.chatter = &decisionStubChatter{response: &Response{Content: "pretty safe"}}

	_, err := d.ScoreCommand(context.Background(), "command: ls\n")
	if !errors.Is(err, ErrDecisionParse) {
		t.Fatalf("err = %v, want ErrDecisionParse (never a guessed score)", err)
	}
}
