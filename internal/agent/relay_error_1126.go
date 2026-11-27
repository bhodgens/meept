package agent

import (
	"errors"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// RelayError (bughunt 2026-11-24 M8) carries a user-facing sentence
// together with the typed error the relay lost on the way here.
//
// The task-relay boundary turns failures into strings:
// handleTaskCompleted (handler.go) takes payload.Result / payload.Error off
// JSON and does UserFacingError(errors.New(relayError)), so every typed lane
// inside UserFacingError (llm.IsContextTooSmall, llm.IsTierFloorError, the
// errors.Is quota/empty checks) sees a brand-new *errors.errorString and
// dies — the user gets the sanitized raw text instead of the actionable
// sentence. Error() returns Text verbatim, so user copy is unchanged;
// Unwrap exposes Cause so errors.As/Is classifiers survive the hop.
type RelayError struct {
	// Text is the user-facing sentence (what the wire carries today).
	Text string
	// Cause is the typed error recovered from the relayed text, or nil
	// when the text carried no recognizable machine sentinel.
	Cause error
}

// Error returns the user sentence, never the raw sentinel internals.
func (r *RelayError) Error() string {
	if r == nil {
		return ""
	}
	return r.Text
}

// Unwrap exposes the typed cause so errors.As / errors.Is reach it.
func (r *RelayError) Unwrap() error {
	if r == nil {
		return nil
	}
	return r.Cause
}

// Machine-sentinel shapes this package can restore from relayed text. The
// llm error types render as "<sentinel>: provider=... model=..." (see
// llm.ContextTooSmallError.Error / llm.TierFloorError.Error), so a prefix
// match is exact by construction: the sentinel text itself is constant.
var (
	relayContextTooSmallRe = regexp.MustCompile(`(?i)\best_input=(\d+)\b.*?\bcontext_limit=(\d+)\b`)
	relayProviderIDRe      = regexp.MustCompile(`(?i)\bprovider=([^\s:,]+)`)
	relayModelIDRe         = regexp.MustCompile(`(?i)\bmodel=([^\s:,]+)`)
)

// relayCause restores a typed error from relayed machine text, best-effort.
// It recognizes ONLY the constants this repo mints (never user prose): a
// context-too-small verdict, a tier-floor verdict, quota-blocked, and empty
// response. The returned errors satisfy llm.IsContextTooSmall /
// llm.IsTierFloorError via errors.As, and llm.ErrAllModelsQuotaBlocked /
// llm.ErrEmptyResponse via errors.Is. Numeric fields are parsed when the
// text carries them; absent fields stay zero rather than fabricated.
func relayCause(text string) error {
	if text == "" {
		return nil
	}
	switch {
	case strings.HasPrefix(text, llm.ErrContextTooSmall.Error()):
		te := &llm.ContextTooSmallError{}
		if m := relayContextTooSmallRe.FindStringSubmatch(text); m != nil {
			te.EstInput = atoiSafe(m[1])
			te.ContextLimit = atoiSafe(m[2])
		}
		te.ProviderID = firstSubmatch(relayProviderIDRe, text)
		te.ModelID = firstSubmatch(relayModelIDRe, text)
		return te
	case strings.HasPrefix(text, llm.ErrTierFloor.Error()):
		te := &llm.TierFloorError{Cause: errors.New(text)}
		te.ProviderID = firstSubmatch(relayProviderIDRe, text)
		te.ModelID = firstSubmatch(relayModelIDRe, text)
		return te
	case strings.HasPrefix(text, llm.ErrAllModelsQuotaBlocked.Error()):
		return llm.ErrAllModelsQuotaBlocked
	case strings.HasPrefix(text, llm.ErrEmptyResponse.Error()):
		return llm.ErrEmptyResponse
	}
	return nil
}

// firstSubmatch returns capture group 1 of the first match, or "".
func firstSubmatch(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// atoiSafe parses a positive int, returning 0 on any noise (relay text is
// user-adjacent input; a parse failure must never break the error path).
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 1<<31-1 {
			return 0
		}
	}
	return n
}
