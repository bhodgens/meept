// CodexClient typed HTTP-status errors (codex 429/API-status classification).
//
// The codex backend is OpenAI-compatible at the error layer, so non-200
// responses are classified exactly like client.go's OpenAI-shaped path:
//
//	429 → *RateLimitError (Retry-After header honored) or *QuotaResetError
//	      when the body carries a usage-window/billing shape (leaf-01 rule —
//	      the quota NonRetryable early-exit upstream of every retry loop
//	      depends on this), wrapping a *APIError as its Cause
//	other non-200 → *APIError{StatusCode, Detail}
//
// This file lives outside errors.go (owned by another workstream); it only
// CONSUMES the package's existing public parse helpers.
package llm

import (
	"net/http"
)

// codexErrorFromResponse converts a non-200 CodexClient HTTP exchange into
// the same typed error lane the OpenAI-compat client produces, so 429s hit
// the RateLimitError classification (IsRateLimitError / rotate-lane) and
// quota-window bodies hit the NonRetryable early-exit instead of a generic
// ClientError that no downstream errors.Is/errors.As lane recognizes.
//
// respBody: raw body bytes (read by the caller ONLY on the non-streaming
// path; the streaming path passes nil/empty — there is no buffered body).
// retryAfterHeader: the raw Retry-After header value ("" when absent).
// providerID/modelID identify the credential for error metadata.
//
// Retry-After parsing reuses errors.go's package-local parseRetryAfter —
// same package, so the shared parser is consumed, not duplicated.
func codexErrorFromResponse(statusCode int, respBody []byte, retryAfterHeader, providerID, modelID string) error {
	detail := string(respBody)
	const maxDetail = 500
	if len(detail) > maxDetail {
		detail = detail[:maxDetail]
	}

	if statusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(retryAfterHeader)

		// Structured 429 metadata (OpenRouter / generic {error:{...}} JSON).
		var providerDetail *ProviderError
		if len(respBody) > 0 {
			providerDetail = ParseRateLimitBody(respBody)
		}

		// Quota-window classification FIRST, byte-matching client.go:
		// usage-window/billing shapes must surface as QuotaResetError so
		// every client retry loop's errors.As quota early-exit fires before
		// any RateLimitError short-retry lane (quota windows are hours).
		if classifyQuotaDecision(statusCode, respBody, providerDetail) {
			qe := ParseQuotaResponse(statusCode, nil, respBody, QuotaContext{
				ProviderID: providerID,
				ModelID:    modelID,
			})
			if qe != nil {
				qe.Cause = &APIError{StatusCode: statusCode, Detail: detail}
				return qe
			}
		}

		rlErr := &RateLimitError{
			ProviderID: providerID,
			ModelID:    modelID,
			RetryAfter: retryAfter,
			Cause:      &APIError{StatusCode: statusCode, Detail: detail},
		}
		if providerDetail != nil {
			if retryAfter == 0 && providerDetail.RetryAfter > 0 {
				rlErr.RetryAfter = providerDetail.RetryAfter
			}
			if providerDetail.RetryStrategy != nil && providerDetail.RetryStrategy.Type != "" {
				rlErr.LimitType = providerDetail.RetryStrategy.Type
			} else if providerDetail.LimitBudget != nil {
				rlErr.LimitType = providerDetail.LimitBudget.Window
			}
			rlErr.RetryStrategy = providerDetail.RetryStrategy
			rlErr.LimitBudget = providerDetail.LimitBudget
		}
		return rlErr
	}

	// Every other non-200 is a plain APIError with the status and body
	// detail — matching client.go's non-429 error lane.
	return &APIError{StatusCode: statusCode, Detail: detail}
}
