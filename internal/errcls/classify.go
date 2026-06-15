// Package errcls provides shared error classification helpers that replace
// ad-hoc strings.Contains(err.Error(), ...) checks across the codebase.
//
// All helpers return false on nil errors. They use errors.As / errors.Is so
// wrapping via fmt.Errorf("...: %w", err) is handled correctly.
//
// To avoid import cycles, errcls only depends on internal/llm (a leaf package)
// and the standard library. Higher-level packages that define sentinel errors
// (e.g. internal/services) register them via RegisterSentinels at init time.
package errcls

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"

	"github.com/caimlas/meept/internal/llm"
)

// Registered sentinels for packages that errcls cannot import directly.
var (
	paramSentinels []error
	authSentinels  []error
	sentinelMu     sync.RWMutex
)

// RegisterParameterSentinels registers sentinel errors that IsParameterError
// should treat as parameter-validation errors. Call from init() in packages
// that define parameter-error sentinels (e.g. services.ErrInvalidInput).
// This indirection avoids import cycles.
func RegisterParameterSentinels(errs ...error) {
	sentinelMu.Lock()
	defer sentinelMu.Unlock()
	paramSentinels = append(paramSentinels, errs...)
}

// RegisterAuthSentinels registers sentinel errors that IsAuthError should
// treat as authentication errors. Call from init() in packages that define
// auth-error sentinels (e.g. services.ErrUnauthorized).
func RegisterAuthSentinels(errs ...error) {
	sentinelMu.Lock()
	defer sentinelMu.Unlock()
	authSentinels = append(authSentinels, errs...)
}

// IsRateLimit reports whether err represents an HTTP 429 / provider rate-limit
// error, including wrapped variants.
func IsRateLimit(err error) bool {
	if err == nil {
		return false
	}
	if llm.IsRateLimitError(err) {
		return true
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 429 {
		return true
	}
	return false
}

// IsRetryable reports whether err represents a transient error that warrants
// retry: rate limits, server errors (5xx incl. 529), context deadlines,
// network errors, and other temporary failures. Non-retryable errors
// (budget exceeded, context-size exceeded, 4xx client errors) return false.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if llm.IsNonRetryable(err) {
		return false
	}
	if IsRateLimit(err) {
		return true
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 && apiErr.StatusCode < 600
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Temporary() || netErr.Timeout()) {
		return true
	}
	if isConnError(err) {
		return true
	}
	return false
}

// IsAuthError reports whether err is an authentication/authorization error
// (HTTP 401 or 403, or a registered auth sentinel such as
// services.ErrUnauthorized).
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 401 || apiErr.StatusCode == 403
	}
	sentinelMu.RLock()
	defer sentinelMu.RUnlock()
	for _, sentinel := range authSentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// IsClientError reports whether err is a non-retryable 4xx client error
// (excluding 401/403 which IsAuthError handles separately).
func IsClientError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		return status >= 400 && status < 500 && status != 401 && status != 403
	}
	return false
}

// IsParameterError reports whether err is a parameter-validation error
// suitable for JSON-RPC -32602 InvalidParams. This replaces the substring
// heuristic in rpc/server.go's isParameterError which false-positive'd on
// common words like "type" and "expected".
//
// Structured detection: matches registered parameter sentinels (via
// RegisterParameterSentinels, e.g. services.ErrInvalidInput) and
// *llm.APIError with StatusCode 400. Plain fmt.Errorf strings are NOT
// matched (the old substring heuristic is intentionally dropped to eliminate
// false positives).
func IsParameterError(err error) bool {
	if err == nil {
		return false
	}
	sentinelMu.RLock()
	defer sentinelMu.RUnlock()
	for _, sentinel := range paramSentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		return true
	}
	return false
}

// IsNetworkError reports whether err is a network/connection-level failure
// (connection refused, reset, broken pipe, EOF, closed). Used by clients
// (TUI RPC, daemon transport) to decide whether to reconnect.
//
// This catches the structured syscall.Errno / io.EOF / net.ErrClosed cases.
// Errors whose .Error() text merely contains the words "connection refused"
// (without wrapping one of these sentinels) are NOT matched — callers that
// produce such errors should wrap the appropriate sentinel instead.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return isConnError(err)
}

// isConnError handles the syscall.Errno cases shared between IsRetryable and
// IsNetworkError. Avoids importing a larger syscall surface in callers.
func isConnError(err error) bool {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, syscall.ENETUNREACH):
		return true
	}
	return false
}
