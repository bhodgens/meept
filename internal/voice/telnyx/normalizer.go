package telnyx

// Tolerant Telnyx messaging-webhook payload normalizer (leaf
// 01-webhook-hardening/02, contract §2).
//
// The legacy parser this replaces (internal/telnyx webhookEnvelope) typed
// from/to as PLAIN STRINGS and could not parse the REAL Telnyx messaging
// payload: `from` is an OBJECT {"phone_number": ...} and `to` is an ARRAY
// of objects. This file ports the vetted extractors + status map from the
// owner's strategist-usa repo (D1):
//   - extractStringOrPhoneField / extractToField  <- pkg/services/telnyx_provider.go:674-706
//   - status map                                   <- go-web-hooks/internal/webhook/normalizer.go:247-293
//   - normalizeE164                                <- go-web-hooks/internal/webhook/normalizer.go:297-325
//   - media scheme filter + missing-payload-id guard <- normalizeTelnyx :186-200, :140-142
// Field identities (normalizer.go:221, :228): EventID = data.id (the
// webhook EVENT uuid, the nonce/dedupe key), ProviderMessageID =
// data.payload.id (the provider MESSAGE uuid, the DLR resolution key).
//
// meept deviations from the port (all pinned by §P):
//   - message.finalized passes payload.status through VERBATIM
//     (delivered|failed|undelivered|expired) — meept KEEPS the raw value
//     instead of strategist's collapsed map (ratified reading).
//   - unknown/noop event types surface as ErrUnknownEvent (errors.Is)
//     so the handler can 200-ack them; MALFORMED JSON returns a distinct
//     error so the handler can 400 it (posture table §H).
//   - NormalizeE164US returns "" for inputs with no digits at all (the
//     port's fallback would emit a meaningless "+").

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUnknownEvent signals an event type this parser does not consume —
// both the known-noop family (message.created/deleted, non-SMS types)
// and genuinely unknown types. The handler 200-acks it to stop Telnyx
// redelivery (posture table); it MUST NOT match for malformed JSON,
// which the handler maps to 400 after signature verification.
var ErrUnknownEvent = errors.New("telnyx: unknown messaging event type")

// Message is the normalized Telnyx messaging webhook event — the shape
// the comm/http webhook seams (OnInbound/OnFinalized) consume.
type Message struct {
	Event     string // RAW data.event_type ("message.received", ...)
	EventID   string // data.id — webhook event uuid, the nonce/dedupe key
	Direction string // "inbound" (message.received, by EVENT TYPE) | "outbound"
	Status    string // sent|queued|sending verbatim; finalized passes
	//             payload.status through verbatim (delivered|failed|
	//             undelivered|expired); failed/delivery_failed -> "error";
	//             message.received -> "".
	IssuedAt time.Time // data.occurred_at (RFC3339; zero if absent/unparseable)
	From     string    // normalized E.164, US-biased (NormalizeE164US)
	To       string    // normalized E.164, US-biased (first `to` entry)
	Text     string
	// ProviderMessageID is data.payload.id — the provider message uuid
	// the send API returns, the DLR resolution key (the master contract's
	// "// data.id" comment refers to the payload data object).
	ProviderMessageID string
	MediaURLs         []string // scheme-filtered http/https only, order preserved
	Errors            []struct {
		Code        string
		Description string // title-first, detail fallback (normalizeTelnyx :196-201)
	}
}

// knownMessagingEventTypes are the types ParseMessagingEvent consumes.
// Everything else — including the known-noop family (message.created,
// message.deleted, non-SMS types) — returns ErrUnknownEvent so the
// handler 200-acks and redelivery stops.
var knownMessagingEventTypes = map[string]bool{
	"message.received":        true,
	"message.sent":            true,
	"message.queued":          true,
	"message.sending":         true,
	"message.finalized":       true,
	"message.failed":          true,
	"message.delivery_failed": true,
}

// ParseMessagingEvent parses a raw Telnyx messaging webhook body into a
// normalized Message. Ordering the handler's posture depends on:
// malformed JSON -> error NOT matching ErrUnknownEvent (400 after
// verify); parseable envelope with an unhandled/unknown/noop event type
// -> error matching ErrUnknownEvent (200 ack); recognized messaging event
// with a payload missing its id -> ordinary error (the DLR key is
// mandatory — ported guard from normalizeTelnyx :140-142).
func ParseMessagingEvent(raw []byte) (Message, error) {
	var env struct {
		Data struct {
			EventType  string          `json:"event_type"`
			ID         string          `json:"id"`
			OccurredAt string          `json:"occurred_at"`
			Payload    json.RawMessage `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		// Distinct from ErrUnknownEvent: the handler maps this 400.
		return Message{}, fmt.Errorf("telnyx messaging webhook: malformed JSON: %w", err)
	}

	eventType := env.Data.EventType
	// Classify BEFORE the payload-id guard: a noop/unknown body (e.g. a
	// call.* event, whose payload legitimately has no messaging id) is an
	// ErrUnknownEvent ack, never a 400.
	if !knownMessagingEventTypes[eventType] {
		return Message{}, fmt.Errorf("%w: %q", ErrUnknownEvent, eventType)
	}

	// Tolerant payload decode: unknown fields and alternate type forms
	// (from string|object, to string|object|array) must never break the
	// parse. UseNumber keeps numeric JSON values exact for the loose
	// string coercions below (no float64 mangling of error codes).
	payload := map[string]any{}
	if len(env.Data.Payload) > 0 {
		dec := json.NewDecoder(bytes.NewReader(env.Data.Payload))
		dec.UseNumber()
		if err := dec.Decode(&payload); err != nil {
			return Message{}, fmt.Errorf("telnyx messaging webhook: payload not an object: %w", err)
		}
	}

	providerMessageID, _ := payload["id"].(string)
	if providerMessageID == "" {
		// Ported guard (normalizeTelnyx :140-142): a recognized messaging
		// event with no payload id has no DLR resolution key — reject it.
		return Message{}, fmt.Errorf("telnyx messaging webhook: event %s missing payload id", eventType)
	}

	msg := Message{
		Event:             eventType,
		EventID:           env.Data.ID,
		ProviderMessageID: providerMessageID,
		Direction:         "outbound",
	}
	if eventType == "message.received" {
		// Direction is decided by the EVENT TYPE, never payload.direction
		// (normalizer.go:249-250).
		msg.Direction = "inbound"
	}

	msg.From = NormalizeE164US(extractStringOrPhoneField(payload["from"]))
	msg.To = NormalizeE164US(extractToField(payload["to"]))

	msg.Text, _ = payload["text"].(string)

	// Status map (normalizer.go:247-293) with the meept-pinned variants
	// (contract §2): sent/queued/sending pass the payload status through;
	// finalized keeps payload.status VERBATIM (delivered|failed|
	// undelivered|expired — no collapse); failed/delivery_failed map to
	// the terminal "error" marker; received carries no status.
	payloadStatus, _ := payload["status"].(string)
	switch eventType {
	case "message.received":
		msg.Status = ""
	case "message.sent", "message.queued", "message.sending":
		msg.Status = payloadStatus
	case "message.finalized":
		msg.Status = payloadStatus
	case "message.failed", "message.delivery_failed":
		msg.Status = "error"
	}

	msg.IssuedAt = parseOccurredAt(env.Data.OccurredAt)
	msg.MediaURLs = extractMediaURLs(payload["media"])
	msg.Errors = extractEventErrors(payload["errors"])
	return msg, nil
}

// parseOccurredAt parses data.occurred_at (RFC3339). An unparseable or
// absent timestamp is NOT fatal — the DLR outcome is what the chain
// consumes — leaving the zero time (mirrors the legacy parser's stance).
func parseOccurredAt(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts
	}
	return time.Time{}
}

// extractStringOrPhoneField extracts a phone number from a field that can
// be either a plain string or a {"phone_number": "..."} object. Ported
// from strategist-usa pkg/services/telnyx_provider.go:674-689; the
// junk-value fallback returns "" (meept adaptation: a "%"v" of a JSON
// blob is never a phone number).
func extractStringOrPhoneField(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["phone_number"].(string); ok {
			return s
		}
	}
	return ""
}

// extractToField extracts the first phone number from the "to" field,
// which can be a string, an array of objects, an array of strings, or a
// bare object. Ported from strategist-usa telnyx_provider.go:691-706; an
// EMPTY array yields "" (first-entry guard — §P pins To=="" for to=[]).
func extractToField(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []any:
		if len(val) > 0 {
			return extractStringOrPhoneField(val[0])
		}
		return ""
	case map[string]any:
		return extractStringOrPhoneField(val)
	}
	return ""
}

// extractMediaURLs keeps only non-empty http/https media URLs, order
// preserved (scheme filter ported from normalizeTelnyx :186-200; ftp and
// data: URIs are provider-injection surfaces and drop out).
func extractMediaURLs(v any) []string {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	var urls []string
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		u, _ := m["url"].(string)
		if u == "" {
			continue
		}
		lower := strings.ToLower(u)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			urls = append(urls, u)
		}
	}
	return urls
}

// extractEventErrors normalizes the payload errors array into
// {Code, Description} pairs. Description is title-first with a detail
// fallback (strategist normalizeTelnyx :196-201 errorMessage semantics);
// Code accepts JSON strings or numbers via the loose coercion below.
func extractEventErrors(v any) []struct {
	Code        string
	Description string
} {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	var out []struct {
		Code        string
		Description string
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var desc string
		if title, _ := m["title"].(string); title != "" {
			desc = title
		} else if detail, _ := m["detail"].(string); detail != "" {
			desc = detail
		}
		out = append(out, struct {
			Code        string
			Description string
		}{Code: looseString(m["code"]), Description: desc})
	}
	return out
}

// looseString renders a JSON scalar (string, json.Number from
// UseNumber, or bool) as a string; nested/object values yield "".
func looseString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	case bool:
		if val {
			return "true"
		}
		return "false"
	}
	return ""
}

// NormalizeE164US normalizes a phone number toward E.164 with a US bias.
// Ported from strategist-usa go-web-hooks
// internal/webhook/normalizer.go:297-325: trim, keep the
// plus, strip EVERY non-digit (extension digits concatenate — port
// semantics), then: hasPlus or 11-digit-leading-1 -> "+digits"; bare
// 10-digit -> "+1"+digits (US assumption); everything else -> "+digits".
// meept adaptation: an input with NO digits at all returns "" rather
// than the port's meaningless "+".
func NormalizeE164US(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	hasPlus := strings.HasPrefix(s, "+")

	var digits strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	result := digits.String()
	if result == "" {
		return ""
	}

	if hasPlus || (len(result) == 11 && result[0] == '1') {
		return "+" + result
	}
	// Assume US number if 10 digits.
	if len(result) == 10 {
		return "+1" + result
	}
	return "+" + result
}
