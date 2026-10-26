// Package githubsvc provides the GitHub PR/CI service layer: token
// resolution, a per-token client pool, and PR status summarization for
// the github-pr-ci tool tree.
package githubsvc

import (
	"context"
	"errors"
	"fmt"

	"github.com/caimlas/meept/internal/secrets"
)

// TokenSource resolves a GitHub API token for a request context.
type TokenSource interface {
	// Token returns the resolved token, or an error. Implementations
	// whose failure is due to a missing/placeholder secret must wrap
	// ErrNoToken (via errors.Is-visible wrapping) so callers can
	// fail closed on the sentinel.
	Token(ctx context.Context) (string, error)
}

// ErrNoToken is the fail-closed sentinel: any GitHub operation must
// refuse to run when no real token could be resolved.
var ErrNoToken = errors.New("githubsvc: no GitHub token resolved")

// SecretsTokenSource resolves a token from the secrets Broker's child
// value for SecretName.
type SecretsTokenSource struct {
	Broker     *secrets.Broker
	SecretName string
}

// Token implements TokenSource.
func (s SecretsTokenSource) Token(ctx context.Context) (string, error) {
	if s.Broker == nil {
		return "", fmt.Errorf("%w: no secrets broker configured for %q", ErrNoToken, s.SecretName)
	}
	val, err := s.Broker.ChildValue(s.SecretName)
	if err != nil {
		return "", fmt.Errorf("githubsvc: resolve secret %q: %w: %w", s.SecretName, ErrNoToken, err)
	}
	if val == "" || secrets.IsPlaceholder(val) {
		return "", fmt.Errorf("%w: secret %q has no real value", ErrNoToken, s.SecretName)
	}
	return val, nil
}
