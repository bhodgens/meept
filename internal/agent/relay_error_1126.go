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
// llm.ContextTooSmallError.Error / llm.TierFloorError.Error).
//
// Fixwave 2026-11-27 (bughunt A-4/M6): the match is strings.Contains, not
// HasPrefix. Every production producer of a failed task's relayed result
// WRAPS the verdict before it reaches this seam — components.go
// "agent execution failed: %w", tactical.go "step X has validation error:",
// "reviewer error: ", and the planner's "planning failed: " lane — so a
// prefix match never fired on real relay text and the M8 typing restore was
// dead in production. The sentinel constants are unique repo-minted strings
// this package never accepts from user prose (the garbage pin below), so
// containment cannot over-match a non-context-too-small error: the verdict
// carries the structured provider=/model=/est_input= tail the regexes parse,
// and no producer legitimately embeds the sentinel inside a different error
// class (grep-verified across the producers above).
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
	case strings.Contains(text, llm.ErrContextTooSmall.Error()):
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
