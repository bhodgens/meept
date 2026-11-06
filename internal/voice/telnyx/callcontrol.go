package telnyx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Codec is the bidirectional audio codec requested on streaming_start.
// Default is L16 at 16000 Hz; the PCMU/8000 fallback is supported.
type Codec struct {
	BidirectionalCodec string
	SamplingRate       int
}

// DefaultCodec is L16 at 16000 Hz, the meept bridge's native codec.
var DefaultCodec = Codec{BidirectionalCodec: "L16", SamplingRate: 16000}

// PCMUCodec is the PCMU at 8000 Hz fallback.
var PCMUCodec = Codec{BidirectionalCodec: "PCMU", SamplingRate: 8000}

// CallEvent is one parsed Telnyx call-control webhook. EventType is one of
// "call.initiated" | "call.answered" | "call.hangup" (plus the streaming.*
// lifecycle events, which parse as no-ops). From and To are the plain
// E.164 strings when Telnyx sends them flat; the nested
// {"phone_number": "..."} shape is also unwrapped by ParseCallEvent.
type CallEvent struct {
	EventType     string // "call.initiated" | "call.answered" | "call.hangup"
	CallControlID string
	CallSessionID string
	From, To      string
	Direction     string // "incoming" | "outgoing"
}

// flexibleString accepts either a plain JSON string ("+15551230000") or an
// object carrying {"phone_number": "..."}, which Telnyx uses in some
// payload versions. Parsing a payload.field that is neither is an error.
type flexibleString string

func (s *flexibleString) UnmarshalJSON(b []byte) error {
	var plain string
	if err := json.Unmarshal(b, &plain); err == nil {
		*s = flexibleString(plain)
		return nil
	}
	var obj struct {
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.Unmarshal(b, &obj); err == nil {
		*s = flexibleString(obj.PhoneNumber)
		return nil
	}
	return fmt.Errorf("telnyx call event: from/to field is neither a string nor {phone_number}")
}

type callEventEnvelope struct {
	Data struct {
		EventType string `json:"event_type"`
		Payload   struct {
			CallControlID string         `json:"call_control_id"`
			CallSessionID string         `json:"call_session_id"`
			From          flexibleString `json:"from"`
			To            flexibleString `json:"to"`
			Direction     string         `json:"direction"`
		} `json:"payload"`
	} `json:"data"`
}

// ParseCallEvent reads a canonical Telnyx call webhook
// {"data":{"event_type":...,"payload":{...}}} into a CallEvent. Invalid JSON
// and any event type other than call.initiated/answered/hangup are errors, so
// a misrouted webhook (message.received, call.fax.*, ...) can never be acted
// on as if it were a live call. The streaming.* lifecycle events parse as
// no-ops (event type preserved): rejecting them would trigger Telnyx
// redelivery of a webhook that needs no action (observed live in uptractor).
func ParseCallEvent(body []byte) (CallEvent, error) {
	var env callEventEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return CallEvent{}, fmt.Errorf("telnyx call event: %w", err)
	}
	switch env.Data.EventType {
	case "call.initiated", "call.answered", "call.hangup",
		// Telnyx reports streaming lifecycle on the call webhook too; they
		// carry no action for us (the media websocket already knows), so
		// parse them as no-op events instead of rejecting the webhook and
		// triggering Telnyx redelivery (observed live in uptractor,
		// 2026-10-28).
		"streaming.started", "streaming.stopped", "streaming.failed":
	default:
		return CallEvent{}, fmt.Errorf("telnyx call event: unknown event_type %q", env.Data.EventType)
	}
	return CallEvent{
		EventType:     env.Data.EventType,
		CallControlID: env.Data.Payload.CallControlID,
		CallSessionID: env.Data.Payload.CallSessionID,
		From:          string(env.Data.Payload.From),
		To:            string(env.Data.Payload.To),
		Direction:     env.Data.Payload.Direction,
	}, nil
}

// streamRequest is the payload POSTed for the streaming_start action.
type streamRequest struct {
	StreamURL   string `json:"stream_url"`
	StreamTrack string `json:"stream_track"`
	// BidirectionalRTP enables Telnyx bidirectional streaming so audio the
	// bridge writes back is actually played to the caller. Default
	// streaming is one-way: return frames are silently dropped and the
	// caller hears nothing (uptractor first live drill, 2026-10-28).
	BidirectionalMode string `json:"stream_bidirectional_mode,omitempty"`
	// Codec is case-sensitive per the Telnyx API; set explicitly so no
	// unexpected transcode happens.
	BidirectionalCodec string `json:"stream_bidirectional_codec,omitempty"`
	BidirectionalRate  int    `json:"stream_bidirectional_sampling_rate,omitempty"`
}

// StartStreaming asks Telnyx to stream the caller's inbound audio to
// streamURL (a ws:// or wss:// media socket) and to accept the bridge's
// outbound audio over the same socket: stream_bidirectional_mode=rtp with
// the codec's bidirectional codec and sampling rate. Only the inbound
// track is streamed; the reply is written back over the same socket.
func (c *Client) StartStreaming(ctx context.Context, callControlID, streamURL string, codec Codec) error {
	if codec.BidirectionalCodec == "" {
		codec.BidirectionalCodec = DefaultCodec.BidirectionalCodec
	}
	if codec.SamplingRate == 0 {
		codec.SamplingRate = DefaultCodec.SamplingRate
	}
	payload, err := json.Marshal(streamRequest{
		StreamURL:          streamURL,
		StreamTrack:        "inbound_track",
		BidirectionalMode:  "rtp",
		BidirectionalCodec: codec.BidirectionalCodec,
		BidirectionalRate:  codec.SamplingRate,
	})
	if err != nil {
		return fmt.Errorf("telnyx start streaming: encode: %w", err)
	}
	_, err = c.postCall(ctx, "start streaming", "/calls/"+url.PathEscape(callControlID)+"/actions/streaming_start", payload)
	return err
}

// dialRequest is the POST /calls body that starts an outbound call.
// timeout_secs is the carrier-side ring limit; the dialer sends its
// resolved no-answer seconds (voice.timeout_secs) so the carrier hangs a
// dead ring up at the same moment the local timer does (audit 2026-11-06
// M8 - the field was previously hardcoded to 30, ignoring config).
type dialRequest struct {
	ConnectionID string `json:"connection_id"`
	To           string `json:"to"`
	From         string `json:"from"`
	TimeoutSecs  int    `json:"timeout_secs"`
}

// DefaultDialTimeoutSecs is the carrier-side timeout_secs fallback when the
// caller passes <= 0: it matches telnyx's own default and voice's
// DefaultNoAnswerSecs, so an unset config keeps the old wire behavior
// (audit 2026-11-06 M8).
const DefaultDialTimeoutSecs = 30

// DialOut starts an outbound call through the Call Control application
// identified by connectionID and returns the new call_control_id parsed
// from data.call_control_id. timeoutSecs is the carrier-side timeout_secs
// (<= 0 falls back to DefaultDialTimeoutSecs, matching telnyx's own
// default). A non-2xx response or an undecodable body is an %w-wrapped
// error with an empty id.
func (c *Client) DialOut(ctx context.Context, to, from, connectionID string, timeoutSecs int) (string, error) {
	if timeoutSecs <= 0 {
		timeoutSecs = DefaultDialTimeoutSecs
	}
	payload, err := json.Marshal(dialRequest{
		ConnectionID: connectionID,
		To:           to,
		From:         from,
		TimeoutSecs:  timeoutSecs,
	})
	if err != nil {
		return "", fmt.Errorf("telnyx dial out: encode: %w", err)
	}
	respBody, err := c.postCall(ctx, "dial out", "/calls", payload)
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			CallControlID string `json:"call_control_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("telnyx dial out: decode: %w", err)
	}
	return out.Data.CallControlID, nil
}

// CallController is the telnyx-facing seam the call manager consumes.
// *Client satisfies it.
type CallController interface {
	AnswerCall(ctx context.Context, callControlID string) error
	StartStreaming(ctx context.Context, callControlID, streamURL string, codec Codec) error
	HangupCall(ctx context.Context, callControlID string) error
}

// compile-time proof that *Client satisfies CallController.
var _ CallController = (*Client)(nil)
