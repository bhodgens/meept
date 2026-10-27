// Package githubsvc provides the GitHub PR/CI service layer: token
// resolution, a per-token client pool, and PR status summarization for
// the github-pr-ci tool tree.
package githubsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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

// StaticTokenSource returns a fixed token string, resolved eagerly at
// construction. The token is NEVER logged; use it only where the value
// comes from memory that is already trusted (tests, one-shot CLI invocations).
type StaticTokenSource struct {
	token string
}

// NewStaticTokenSource wraps a literal token. An empty/whitespace token is
// a fail-closed registration error, mirroring FileTokenSource.
func NewStaticTokenSource(token string) (TokenSource, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("%w: static token is empty", ErrNoToken)
	}
	return &StaticTokenSource{token: token}, nil
}

// Token implements TokenSource.
func (s *StaticTokenSource) Token(ctx context.Context) (string, error) {
	return s.token, nil
}

// ErrTokenSource always returns the configured error. The daemon uses it
// for a configured secret that cannot be resolved at registration time
// (no secrets broker wired, token file with unsafe perms): the tools still
// register for membership but fail closed at every request with this
// named error, instead of silently going unauthenticated.
type ErrTokenSource struct {
	// Err is returned by every Token call. Must name the missing source.
	Err error
}

// Token implements TokenSource.
func (s ErrTokenSource) Token(ctx context.Context) (string, error) {
	if s.Err == nil {
		return "", fmt.Errorf("%w: no token source configured", ErrNoToken)
	}
	return "", s.Err
}

// FileTokenSource resolves a GitHub token from a file on disk. The file
// must be readable and carry a non-empty token (surrounding whitespace is
// trimmed); a missing or empty file is a fail-closed error wrapping
// ErrNoToken. The path is resolved by the caller (expand ~ / MEEPT_HOME
// before handing it here); this source does no path rewriting.
type FileTokenSource struct {
	// Path is the absolute (tilde-expanded) token file to read.
	Path string
}

// Token implements TokenSource.
func (s FileTokenSource) Token(ctx context.Context) (string, error) {
	if s.Path == "" {
		return "", fmt.Errorf("%w: no token file path configured", ErrNoToken)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return "", fmt.Errorf("%w: read token file: %v", ErrNoToken, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("%w: token file %s is empty", ErrNoToken, s.Path)
	}
	return token, nil
}

// SecretsTokenSource resolves a token from the secrets Broker's REAL value
// for SecretName (Broker.Value — the in-process accessor). Placeholder and
// missing values fail closed with ErrNoToken. The token is never logged.
type SecretsTokenSource struct {
	Broker     *secrets.Broker
	SecretName string
}

// Token implements TokenSource.
func (s SecretsTokenSource) Token(ctx context.Context) (string, error) {
	if s.Broker == nil {
		return "", fmt.Errorf("%w: no secrets broker configured for %q", ErrNoToken, s.SecretName)
	}
	val, err := s.Broker.Value(s.SecretName)
	if err != nil {
		return "", fmt.Errorf("githubsvc: resolve secret %q: %w: %w", s.SecretName, ErrNoToken, err)
	}
	if val == "" || secrets.IsPlaceholder(val) {
		return "", fmt.Errorf("%w: secret %q has no real value", ErrNoToken, s.SecretName)
	}
	return val, nil
}
