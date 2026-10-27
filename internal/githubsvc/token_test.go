package githubsvc

// NOTE: gate test from the github-pr-ci tree (leaf 00T, Task 1, F2).
// Verifies SecretsTokenSource.Token against a REAL secrets.Broker built the
// same way internal/secrets's own tests build one (NewBroker over env
// sources). Covers all four leaf-00T cases: a resolvable real value returns
// it; placeholder-form, nil-broker, and unknown-secret paths fail closed
// with ErrNoToken (errors.Is).

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/secrets"
)

func TestSecretsTokenSource_Resolves(t *testing.T) {
	t.Setenv("GATE_TEST_TOKEN_ENV", "ghs_gate_real_value")

	newDiscard := func() *slog.Logger { return slog.New(slog.DiscardHandler) }

	// Broker with one known secret loaded from the env source above, plus
	// one whose stored value is placeholder-shaped (a misconfigured source).
	t.Setenv("GATE_TEST_TOKEN_PLACEHOLDER", secrets.Placeholder("other"))
	broker2, err := secrets.NewBroker(secrets.Config{
		"github_token":  {Kind: "env", Name: "GATE_TEST_TOKEN_ENV"},
		"broken_secret": {Kind: "env", Name: "GATE_TEST_TOKEN_PLACEHOLDER"},
	}, newDiscard())
	if err != nil {
		t.Fatalf("NewBroker(2): %v", err)
	}

	ctx := context.Background()

	t.Run("real value resolves", func(t *testing.T) {
		tok, err := SecretsTokenSource{Broker: broker2, SecretName: "github_token"}.Token(ctx)
		if err != nil {
			t.Fatalf("Token() = %q, %v; want the real secret value", tok, err)
		}
		if tok != "ghs_gate_real_value" {
			t.Fatalf("Token() = %q, want the real secret value", tok)
		}
	})

	failCases := []struct {
		name string
		src  SecretsTokenSource
	}{
		{
			// nil broker: fail closed, no dereference panic.
			name: "nil broker wraps ErrNoToken",
			src:  SecretsTokenSource{Broker: nil, SecretName: "github_token"},
		},
		{
			// unknown secret name: Broker.Value errors, error wraps
			// ErrNoToken.
			name: "unknown secret name wraps ErrNoToken",
			src:  SecretsTokenSource{Broker: broker2, SecretName: "no_such_secret"},
		},
		{
			// empty secret name against a real broker: unknown -> ErrNoToken.
			name: "empty secret name wraps ErrNoToken",
			src:  SecretsTokenSource{Broker: broker2, SecretName: ""},
		},
		{
			// placeholder-form stored value (a misconfigured source whose
			// content IS a placeholder): not a token — fail closed.
			name: "placeholder value wraps ErrNoToken",
			src:  SecretsTokenSource{Broker: broker2, SecretName: "broken_secret"},
		},
	}

	for _, tc := range failCases {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := tc.src.Token(ctx)
			if err == nil {
				t.Fatalf("Token() = %q, want error wrapping ErrNoToken", tok)
			}
			if !errors.Is(err, ErrNoToken) {
				t.Fatalf("Token() error = %v, want errors.Is(err, ErrNoToken)", err)
			}
			if tok != "" {
				t.Errorf("Token() = %q on failure, want empty string", tok)
			}
			// No real secret material may leak into the error text.
			if strings.Contains(err.Error(), "ghs_gate_real_value") {
				t.Errorf("error %q leaked secret material", err)
			}
		})
	}
}
