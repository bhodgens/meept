package agent

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// Sentences for the user-language error funnel (master C-4,
// 20261121-interactive-usability leaf 06). Exported so leaves 03/05 can
// reuse the exact wording on card texts; the funnel is the only caller on
// the live path.
const (
	// ErrSentenceContextTooSmall: a context-too-small verdict means the
	// whole conversation no longer fits the served model's window.
	ErrSentenceContextTooSmall = "conversation too long for this model; start a new session or ask me to summarize"
	// ErrSentenceTierFloor: the tier policy refused every remaining
	// rotation hop; the pool degraded past the configured floor.
	ErrSentenceTierFloor = "model pool degraded too far while retrying; try again shortly"
	// ErrSentenceEmptyResponse: a served-but-blank completion after the
	// client's own short empty-retry budget.
	ErrSentenceEmptyResponse = "the model returned nothing usable; try resending"
	// ErrSentenceQuotaQueued is the EXISTING quota queued sentence from
	// the quota-resume park path (handler.go) -- kept byte-identical and
	// reused so the funnel and the park path never drift.
	ErrSentenceQuotaQueued = "your message is queued and will run automatically when the quota resets."
)

// UserFacingError maps a terminal turn error to one user-language
// sentence. It is the single mapping table for the response.Error funnel:
// known failure families get actionable copy with no machine ids, paths,
// or sentinel strings; unknown errors fall through to a sanitized
// err.Error() (internal paths and provider prefixes stripped, lowercased).
// The existing typed lanes (BudgetExceededError, QuotaResetError,
// ThrottleGiveUpError -- each with their own UserMessage()) keep flowing
// through their dedicated branches in the handler BEFORE this mapping
// runs; the funnel calls this only for errors they did not claim.
func UserFacingError(err error) string {
	if err == nil {
		return ""
	}
	// Admission verdict: the whole conversation overflows the served
	// model's context window.
	if llm.IsContextTooSmall(err) {
		return ErrSentenceContextTooSmall
	}
	// Tier-policy floor: degradation depth exhausted while rotating.
	if llm.IsTierFloorError(err) {
		return ErrSentenceTierFloor
	}
	// Every alias candidate quota-blocked (classified terminal error,
	// ErrAllModelsQuotaBlocked family).
	if errors.Is(err, llm.ErrAllModelsQuotaBlocked) {
		return llm.ErrAllModelsQuotaBlocked.Error()
	}
	// Served-but-blank completion after the short retry budget.
	if errors.Is(err, llm.ErrEmptyResponse) {
		return ErrSentenceEmptyResponse
	}
	// Gate policy block: deterministic, never retried; the wrapped cause
	// is the user-language gate reason.
	if errors.Is(err, ErrGateBlocked) {
		return sanitizeUserFacing(err.Error())
	}
	// Unknown failure: sanitized raw text, same as the pre-funnel default
	// but with machine noise stripped.
	return sanitizeUserFacing(err.Error())
}

// sanitizeUserFacing strips internal machine vocabulary from a raw error
// string: filesystem/project paths, module prefixes, and provider-addr
// noise that would otherwise leak into a chat reply.
func sanitizeUserFacing(s string) string {
	out := s
	// Absolute paths (any segment starting with / up to whitespace or
	// quote) collapse to their basename.
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "/") || strings.HasPrefix(f, `"/`) {
			clean := strings.Trim(f, `"'(),:`)
			if filepath.IsAbs(clean) {
				out = strings.ReplaceAll(out, f, filepath.Base(clean))
			}
		}
	}
	// Module path prefixes on wrapped sentinels:
	// "github.com/caimlas/meept/internal/agent: message" -> "message".
	if i := strings.LastIndex(out, "/internal/"); i >= 0 {
		if j := strings.Index(out[i:], ": "); j >= 0 {
			out = out[i+j+2:]
		}
	}
	out = strings.TrimSpace(out)
	return out
}
