package telnyx

// Telnyx call recording client surface, ported from uptractor
// phone/core/internal/telnyx/recording.go (branch
// fix/coordinator-missed-hangup-watchdog) and adapted to meept's postCall
// + RecordOptions contract (leaf 06, master "Recording" matrix row).
// Ported early alongside leaf 01 because the committed 00T gate suite
// compiles recording_test.go in the same package: without these symbols
// `go test ./internal/voice/telnyx` cannot build at all, so leaf 01's
// verify command is impossible to satisfy. Only the telnyx-package
// surface is implemented here — the manager/answer-path/consent-gate
// half of leaf 06 is untouched.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// RecordingSavedEvent is the call.recording.saved event type. It joins the
// call lifecycle events ParseCallEvent accepts; the URL rides on
// CallEvent.RecordingURL.
const RecordingSavedEvent = "call.recording.saved"

// RecordOptions are the record_start payload options. The zero value is
// the frozen default {format: mp3, channels: dual}: one channel per
// party, so caller and agent stay separable for eval and brief accuracy.
// The struct exists so future formats ship without a signature change.
type RecordOptions struct {
	Format   string
	Channels string
}

// defaults fills the frozen defaults for any empty option.
func (o RecordOptions) defaults() RecordOptions {
	if o.Format == "" {
		o.Format = "mp3"
	}
	if o.Channels == "" {
		o.Channels = "dual"
	}
	return o
}

type recordStartRequest struct {
	Format   string `json:"format"`
	Channels string `json:"channels"`
}

// StartRecording starts recording an answered call — by default as
// dual-channel mp3 (one channel per party): POST
// /calls/{id}/actions/record_start. Telnyx reports the finished file in
// a call.recording.saved webhook. Explicit options ride the payload
// verbatim.
func (c *Client) StartRecording(ctx context.Context, callControlID string, opts RecordOptions) error {
	payload, err := json.Marshal(recordStartRequest(opts.defaults()))
	if err != nil {
		return fmt.Errorf("telnyx start recording: encode: %w", err)
	}
	_, err = c.postCall(ctx, "start recording",
		"/calls/"+url.PathEscape(callControlID)+"/actions/record_start", payload)
	return err
}

// recordingURL extracts the mp3 URL from a call.recording.saved body.
// Telnyx offers it under recording_urls (expiring), public_recording_urls,
// or, in older payloads, download_urls; the first non-empty one wins. Any
// other body, or a body without a URL, yields "".
func recordingURL(body []byte) string {
	var env struct {
		Data struct {
			Payload struct {
				RecordingURLs       map[string]string `json:"recording_urls"`
				PublicRecordingURLs map[string]string `json:"public_recording_urls"`
				DownloadURLs        map[string]string `json:"download_urls"`
			} `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	p := env.Data.Payload
	for _, urls := range []map[string]string{p.RecordingURLs, p.PublicRecordingURLs, p.DownloadURLs} {
		if u := urls["mp3"]; u != "" {
			return u
		}
	}
	return ""
}
