// Package sessionref implements cross-session bounded references (contract
// S3, .hermes/contracts/03-session-storage.md; leaf 03-session-storage/03).
//
// Ported semantics from dsh @deepseek-ai/dsh-session-reference (uri.ts,
// projection.ts, spill.ts, index.ts). A user message may cite up to
// MAX_REFERENCES other sessions via `@[label](meept-session:<base64url>)`
// mentions; the resolver projects each cited session's user/assistant-only
// conversation into a byte-bounded preview (drop-oldest, then head-tail
// truncate with an exact omitted-bytes notice) and spills the FULL
// transcript to the llm SpillService only when truncation happened.
package sessionref

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// SessionReferenceScheme is the URI scheme reserved for meept session
// snapshots (dsh used `dsh-session:`; contract S3 freezes `meept-session:`).
const SessionReferenceScheme = "meept-session:"

// Sentinel errors (contract S3: a malformed explicit mention fails; the
// error wraps ErrSessionRefInvalid).
var (
	ErrSessionRefInvalid   = errors.New("session reference: invalid reference")
	ErrSessionRefSelf      = errors.New("session reference: self-reference")
	ErrSessionRefTooMany   = errors.New("session reference: too many references")
	ErrSessionRefBudget    = errors.New("session reference: budget exceeded")
	ErrSessionRefRead      = errors.New("session reference: read failed")
	ErrSessionRefCancelled = errors.New("session reference: cancelled")
)

// SessionReferenceInput is one source session selected by a host.
type SessionReferenceInput struct {
	SessionID string
	Label     string
}

// EncodeSessionReferenceURI renders a session ID as its canonical lossless
// URI: `meept-session:<base64url(id)>` (dsh uri.ts encodeSessionReferenceUri,
// minus the JSON string wrapper — the e2e gate pins raw base64url).
func EncodeSessionReferenceURI(sessionID string) string {
	return SessionReferenceScheme + base64.RawURLEncoding.EncodeToString([]byte(sessionID))
}

// DecodeSessionReferenceURI decodes one canonical URI. Non-canonical input
// (re-encode mismatch, invalid base64url) fails with ErrSessionRefInvalid.
func DecodeSessionReferenceURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, SessionReferenceScheme) {
		return "", fmt.Errorf("%w: uri %q has no %q prefix", ErrSessionRefInvalid, uri, SessionReferenceScheme)
	}
	payload := strings.TrimPrefix(uri, SessionReferenceScheme)
	if payload == "" || !isBase64URL(payload) {
		return "", fmt.Errorf("%w: uri %q payload is not base64url", ErrSessionRefInvalid, uri)
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("%s: uri %q: %w", ErrSessionRefInvalid.Error(), uri, err)
	}
	sessionID := string(raw)
	if sessionID == "" {
		return "", fmt.Errorf("%w: uri %q decodes to an empty id", ErrSessionRefInvalid, uri)
	}
	if EncodeSessionReferenceURI(sessionID) != uri {
		return "", fmt.Errorf("%w: uri %q is not canonical", ErrSessionRefInvalid, uri)
	}
	return sessionID, nil
}

// FormatSessionReferenceMention renders the host-neutral Markdown mention
// `@[label](meept-session:<base64url>)`, escaping `\` and `]` in the label.
func FormatSessionReferenceMention(ref SessionReferenceInput) string {
	label := ref.Label
	if label == "" {
		label = ref.SessionID
	}
	label = strings.ReplaceAll(label, `\`, `\\`)
	label = strings.ReplaceAll(label, `]`, `\]`)
	return "@[" + label + "](" + EncodeSessionReferenceURI(ref.SessionID) + ")"
}

var base64URLRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func isBase64URL(s string) bool { return base64URLRe.MatchString(s) }

// mentionRe matches (1) an explicit Markdown mention with a possibly
// escaped label, or (2) a bare canonical URI in plain text (dsh uri.ts
// parseSessionReferenceText pattern).
var mentionRe = regexp.MustCompile(
	`@\[((?:\\.|[^\\\]]*)*)\]\((meept-session:[^\s)]*)\)|(meept-session:[A-Za-z0-9_-]+)`)

// ParseMentionText extracts Markdown mentions and bare canonical URIs from
// one text value. Each match is replaced by a readable `@label` span in the
// returned clean text. An explicit Markdown mention with a malformed URI
// FAILS (wrapped ErrSessionRefInvalid); a bare mention fails the same way.
// References come back in first-appearance order, before deduplication.
func ParseMentionText(text string) (string, []SessionReferenceInput, error) {
	var refs []SessionReferenceInput
	var parseErr error
	clean := mentionRe.ReplaceAllStringFunc(text, func(match string) string {
		if parseErr != nil {
			return match
		}
		groups := mentionRe.FindStringSubmatch(match)
		uri := groups[2]
		rawLabel, explicit := groups[1], groups[2] != ""
		if !explicit {
			uri = groups[3]
		}
		sessionID, err := DecodeSessionReferenceURI(uri)
		if err != nil {
			parseErr = err
			return match
		}
		label := sessionID
		if explicit {
			label = unescapeLabel(rawLabel)
			if label == "" {
				label = sessionID
			}
		}
		refs = append(refs, SessionReferenceInput{SessionID: sessionID, Label: label})
		return "@" + label
	})
	if parseErr != nil {
		return text, nil, parseErr
	}
	return clean, refs, nil
}

func unescapeLabel(label string) string {
	var b strings.Builder
	b.Grow(len(label))
	for i := 0; i < len(label); i++ {
		if label[i] == '\\' && i+1 < len(label) {
			i++
		}
		b.WriteByte(label[i])
	}
	return b.String()
}
