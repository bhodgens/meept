package errcls_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/services"
)

func init() {
	// Register services sentinels so errcls can recognize them without
	// importing services directly (avoids import cycle).
	errcls.RegisterParameterSentinels(services.ErrInvalidInput)
	errcls.RegisterAuthSentinels(services.ErrUnauthorized)
}

func TestIsRateLimit(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit error", &llm.RateLimitError{RetryAfter: 30}, true},
		{"api 429", &llm.APIError{StatusCode: 429}, true},
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"wrapped rate limit", fmt.Errorf("context: %w", &llm.RateLimitError{}), true},
		{"wrapped api 429", fmt.Errorf("ctx: %w", &llm.APIError{StatusCode: 429}), true},
		{"plain error", errors.New("something else"), false},
		{"joined with rate limit", errors.Join(errors.New("context"), &llm.RateLimitError{}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsRateLimit(tt.err); got != tt.want {
				t.Errorf("IsRateLimit(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", &llm.RateLimitError{}, true},
		{"api 429", &llm.APIError{StatusCode: 429}, true},
		{"api 500", &llm.APIError{StatusCode: 500}, true},
		{"api 502", &llm.APIError{StatusCode: 502}, true},
		{"api 503", &llm.APIError{StatusCode: 503}, true},
		{"api 504", &llm.APIError{StatusCode: 504}, true},
		{"api 529 overload", &llm.APIError{StatusCode: 529}, true},
		{"api 400", &llm.APIError{StatusCode: 400}, false},
		{"api 401", &llm.APIError{StatusCode: 401}, false},
		{"api 404", &llm.APIError{StatusCode: 404}, false},
		{"budget exceeded (non-retryable)", &llm.BudgetExceededError{}, false},
		{"context size exceeded (non-retryable)", &llm.ContextSizeExceededError{}, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, true},
		{"net temp error", tempNetErr{}, true},
		{"net timeout error", timeoutNetErr{}, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"EPIPE", syscall.EPIPE, true},
		{"wrapped context deadline", fmt.Errorf("ctx: %w", context.DeadlineExceeded), true},
		{"wrapped econnrefused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"plain internal error", errors.New("internal failure"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"api 401", &llm.APIError{StatusCode: 401}, true},
		{"api 403", &llm.APIError{StatusCode: 403}, true},
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"services unauthorized", services.ErrUnauthorized, true},
		{"wrapped unauthorized", fmt.Errorf("ctx: %w", services.ErrUnauthorized), true},
		{"nil", nil, false},
		{"plain error", errors.New("denied"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsAuthError(tt.err); got != tt.want {
				t.Errorf("IsAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsParameterError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"services invalid input", services.ErrInvalidInput, true},
		{"wrapped invalid input", fmt.Errorf("ctx: %w", services.ErrInvalidInput), true},
		{"plain missing arg", errors.New("missing required parameter"), false}, // EC-1 fix
		{"api 400", &llm.APIError{StatusCode: 400}, true},
		{"api 404", &llm.APIError{StatusCode: 404}, false},
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"nil", nil, false},
		{"internal failure", errors.New("expected 1 result, got 0"), false}, // EC-1 false positive removed
		{"plain 'invalid X'", errors.New("invalid foo"), false},             // EC-1 fix
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsParameterError(tt.err); got != tt.want {
				t.Errorf("IsParameterError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsClientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"api 400", &llm.APIError{StatusCode: 400}, true},
		{"api 404", &llm.APIError{StatusCode: 404}, true},
		{"api 401 (auth, not client)", &llm.APIError{StatusCode: 401}, false},
		{"api 403 (auth, not client)", &llm.APIError{StatusCode: 403}, false},
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"nil", nil, false},
		{"plain error", errors.New("foo"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsClientError(tt.err); got != tt.want {
				t.Errorf("IsClientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"EPIPE", syscall.EPIPE, true},
		{"ECONNABORTED", syscall.ECONNABORTED, true},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH, true},
		{"ENETUNREACH", syscall.ENETUNREACH, true},
		{"EOF", io.EOF, true},
		{"net.ErrClosed", net.ErrClosed, true},
		{"wrapped EOF", fmt.Errorf("ctx: %w", io.EOF), true},
		{"wrapped econnrefused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"joined EOF", errors.Join(errors.New("ctx"), io.EOF), true},
		{"nil", nil, false},
		{"plain error", errors.New("totally unrelated"), false},
		{"plain 'connection refused' string", errors.New("connection refused by host"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errcls.IsNetworkError(tt.err); got != tt.want {
				t.Errorf("IsNetworkError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type tempNetErr struct{}

func (tempNetErr) Error() string   { return "temp net error" }
func (tempNetErr) Timeout() bool   { return true }
func (tempNetErr) Temporary() bool { return true }

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "timeout net error" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return false }
