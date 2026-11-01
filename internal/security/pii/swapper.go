package pii

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// Token framing for the leaf format [PII:<uuid-v4>].
const (
	tokenPrefix = "[PII:"
	tokenSuffix = "]"
)

// NewToken mints a fresh [PII:<uuid-v4>] token via crypto/rand. UUIDv4
// bits: version nibble '4', variant bits '10' (8/9/a/b). There is no
// math/rand fallback - crypto/rand failure is a hard error (predictable
// IDs are worse than a failed turn).
func NewToken() string {
	uuid, err := newUUIDv4()
	if err != nil {
		// Unreachable in practice; never degrade to predictable tokens.
		panic("pii: crypto/rand failure generating token: " + err.Error())
	}
	return tokenPrefix + uuid + tokenSuffix
}

// newUUIDv4 generates a random UUID version 4 per RFC 9562.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Swapper ties the Detector to the SwapStore: Mask replaces detected PII
// with fresh tokens (one Put per token) and Restore delegates to
// ResolveForRestore.
type Swapper struct {
	detector *Detector
	store    *SwapStore
}

// NewSwapper builds a Swapper over the given store with a default
// (all-classes, enabled) Detector.
func NewSwapper(store *SwapStore) *Swapper {
	return &Swapper{
		detector: NewDetector(Config{Enabled: true}),
		store:    store,
	}
}

// NewSwapperWithDetector builds a Swapper with a caller-configured
// Detector (class filtering, pluggable recognizers).
func NewSwapperWithDetector(store *SwapStore, d *Detector) *Swapper {
	return &Swapper{detector: d, store: store}
}

// maskAssignment is one span replaced by a token, in text coordinates.
type maskAssignment struct {
	start, end int
	token      string
}

// Mask replaces every detected PII span in text with a [PII:<uuid-v4>]
// token and records the mapping in the store scoped to sessionID.
// Identical values within one session reuse the same token (deterministic
// within a session, never across sessions). Returns (masked, count).
func (s *Swapper) Mask(text string, sessionID string) (string, int) {
	spans := s.detector.Detect(text)
	if len(spans) == 0 {
		return text, 0
	}

	assigned := make([]maskAssignment, 0, len(spans))
	for _, sp := range spans {
		if tok, ok := s.store.TokenFor(sessionID, sp.Class, sp.Value); ok {
			assigned = append(assigned, maskAssignment{start: sp.Start, end: sp.End, token: tok})
			continue
		}
		tok := NewToken()
		if err := s.store.Put(sessionID, tok, sp.Value, sp.Class); err != nil {
			// A failed Put must not emit an unbacked token: leave the
			// span raw and report the reduced count (never a token that
			// would not restore).
			continue
		}
		assigned = append(assigned, maskAssignment{start: sp.Start, end: sp.End, token: tok})
	}
	if len(assigned) == 0 {
		return text, 0
	}
	return buildMasked(text, assigned), len(assigned)
}

// buildMasked stitches the masked text from start-ordered spans.
func buildMasked(text string, assigned []maskAssignment) string {
	var b strings.Builder
	pos := 0
	for _, a := range assigned {
		b.WriteString(text[pos:a.start])
		b.WriteString(a.token)
		pos = a.end
	}
	b.WriteString(text[pos:])
	return b.String()
}

// Restore replaces all sessionID tokens in text with their originals via
// the store. Tokens from other sessions are left untouched.
func (s *Swapper) Restore(text string, sessionID string) (string, int) {
	return s.store.ResolveForRestore(sessionID, text)
}
