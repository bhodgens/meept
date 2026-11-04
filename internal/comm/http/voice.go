// Voice HTTP surface (leaf 10): the two Telnyx entry points into the
// daemon.
//
//   - POST /api/v1/voice/webhook  - call-control webhooks. Mandatory Ed25519
//     signature over the raw body, then telnyx.ParseCallEvent, then
//     Manager.HandleCallEvent. streaming.* lifecycle events answer 200 as
//     no-ops so Telnyx never redelivers them.
//   - GET  /api/v1/voice/media    - the bidirectional media websocket. The
//     socket signature is verified OVER "{timestamp}|{stream_url}" BEFORE the
//     upgrade (fail 403, no WS), then the start frame is read (skipping the
//     "connected" frame, bounded 30s/50 frames), then the call is handed to
//     Manager.OnMediaStart with an injected per-call SpliceRunner.
//
// AUTH POSTURE: these routes are called by Telnyx, not by GUI clients, so
// they bypass the server's bearer-key auth (see Server.middleware) and are
// gated by MANDATORY signature verification instead. A missing
// voice.telnyx_public_key never makes the signature optional: the routes
// fail closed with 503 (Register logs a loud WARN once at startup).
//
// WS CLASSIFICATION: this file creates no bus subscriptions and publishes
// no bus topics; the media route is a raw websocket endpoint, not a
// bus-relayed chat_message surface (internal/comm/AGENTS.md).
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// WebSocketConn is the narrow media-socket seam the splice runner (leaf 06
// RunSplice) consumes: identical shape to voice.WebSocketConn, redeclared
// here because importing internal/voice from this package is an import
// cycle (voice -> activity -> employee -> bot -> comm/http). Go's
// structural typing makes the two interchangeable at every call site.
type WebSocketConn interface {
	// Read one message. It blocks until a message or error; the
	// implementation must honor ctx cancellation.
	Read(ctx context.Context) (data []byte, err error)
	Write(p []byte) error
	Close() error
}

// Voice webhook and default media route paths. The media path is
// overridable via VoiceRoutesConfig.MediaPath so it can match the path
// embedded in voice.public_stream_url; the webhook path is fixed.
const (
	voiceWebhookPath = "/api/v1/voice/webhook"

	defaultVoiceMediaPath = "/api/v1/voice/media"

	// maxWebhookBytes bounds a webhook body. Telnyx call-control events are
	// a few KB; anything larger is rejected before parsing.
	maxWebhookBytes = 1 << 20
)

// Start-frame read bounds (leaf 10): Telnyx sends a "connected" frame
// first, then the "start" frame. A client that sends neither must not
// stall this goroutine forever, so the read is bounded on BOTH time and
// frame count.
const (
	startFrameTimeout = 30 * time.Second
	maxStartFrames    = 50
)

// VerifyFuncs carries the pre-built Telnyx signature verifiers (leaf 02).
// Both are mandatory; a nil func or an unset public key disables the routes
// (fail-closed 503), never an unverified pass.
type VerifyFuncs struct {
	// Webhook verifies the Ed25519 headers against the raw request body.
	Webhook func(body []byte, headers http.Header) error
	// Media verifies the Ed25519 headers against the exact stream_url the
	// daemon told Telnyx to dial (Manager.StreamURL, never re-derived).
	Media func(headers http.Header, streamURL string) error
}

// StartFrame is the parsed Telnyx media-socket start frame: which call the
// socket belongs to and what format the audio arrives in. TranscodeInbound
// is the PCMU flag: when the negotiated encoding is PCMU the splice must
// transcode inbound mulaw to L16 (leaf 06 codec path).
type StartFrame struct {
	CallControlID string
	Encoding      string
	SampleRate    int
	// TranscodeInbound is true when Encoding is PCMU (case-insensitive).
	TranscodeInbound bool
}

// SpliceRunner is the per-call media path: leaf 14 injects a closure that
// runs voice.RunSplice for this call under the manager's per-call context.
// It blocks until the call's media path ends (context cancel, terminal WS
// read error, or backpressure hangup).
type SpliceRunner func(ctx context.Context, ccid string, conn WebSocketConn, start StartFrame) error

// VoiceRoutesConfig sizes the voice routes. Enabled is derived by the
// daemon wiring from voice.enabled AND a non-empty voice.telnyx_public_key
// (base64 Ed25519, decoded at wiring time); false fail-closes both routes.
type VoiceRoutesConfig struct {
	// MediaPath overrides defaultVoiceMediaPath. Keep it consistent with
	// the path component of voice.public_stream_url.
	MediaPath string
	// Enabled is false when the Telnyx public key is unset: routes answer
	// 503 (fail-closed, never signature-optional).
	Enabled bool
	// SpliceRunner is the per-call media bridge. Nil with Enabled=true is a
	// construction error (NewVoiceRoutes).
	SpliceRunner SpliceRunner
}

// NOTE: the leaf sketch passed *voice.Manager directly, but the real
// dependency graph forbids that import: internal/voice -> activity ->
// employee -> bot -> comm/http (go list: import cycle not allowed). The
// manager therefore crosses this boundary as the narrow VoiceCallManager
// interface below; *voice.Manager (leaf 05) satisfies it and leaf 14
// injects it. telnyx (stdlib-only) is cycle-free and names the event type.
type VoiceCallManager interface {
	// HandleCallEvent drives the call lifecycle (leaf 05).
	HandleCallEvent(ctx context.Context, ev telnyx.CallEvent) error
	// OnMediaStart registers the media session idempotently and runs dial
	// under the per-call context; the first start for a ccid wins.
	OnMediaStart(ccid string, dial func(context.Context) error) error
	// StreamURL returns the exact media URL passed to streaming_start; the
	// media-socket signature is verified against it, never re-derived.
	StreamURL() string
}

// VoiceRoutes serves the two Telnyx endpoints on the daemon's HTTP server
// and (T6) optionally on a second plain-HTTP loopback listener for a
// TLS-terminating tunnel.
type VoiceRoutes struct {
	mgr    VoiceCallManager
	verify VerifyFuncs
	cfg    VoiceRoutesConfig
	log    *slog.Logger
}

// NewVoiceRoutes builds the voice route group. mgr and a populated verify
// pair are required when cfg.Enabled; logger defaults to slog.Default().
func NewVoiceRoutes(mgr VoiceCallManager, verify VerifyFuncs, cfg VoiceRoutesConfig, logger *slog.Logger) (*VoiceRoutes, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Enabled {
		if mgr == nil {
			return nil, errors.New("voice routes: enabled requires a call manager")
		}
		if verify.Webhook == nil || verify.Media == nil {
			return nil, errors.New("voice routes: enabled requires both signature verifiers (telnyx public key unset?)")
		}
		if cfg.SpliceRunner == nil {
			return nil, errors.New("voice routes: enabled requires a splice runner")
		}
	}
	return &VoiceRoutes{mgr: mgr, verify: verify, cfg: cfg, log: logger}, nil
}

// WebhookPath returns the registered webhook route path.
func (v *VoiceRoutes) WebhookPath() string { return voiceWebhookPath }

// MediaPath returns the registered media websocket route path.
func (v *VoiceRoutes) MediaPath() string {
	if v.cfg.MediaPath != "" {
		return v.cfg.MediaPath
	}
	return defaultVoiceMediaPath
}

// HandlesPath reports whether p is one of the two voice routes. The server
// middleware uses it to exempt exactly these paths from bearer-key auth;
// they remain gated by mandatory signature checks inside the handlers.
func (v *VoiceRoutes) HandlesPath(p string) bool {
	return p == v.WebhookPath() || p == v.MediaPath()
}

// serveVoiceRoute dispatches a request to the matching voice handler with
// method enforcement. The Server middleware calls this (bearer-auth exempt
// branch); a wrong method is a 405 before any signature work.
func (v *VoiceRoutes) serveVoiceRoute(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == v.WebhookPath():
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		v.handleWebhook(w, r)
	case r.URL.Path == v.MediaPath():
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		v.handleMedia(w, r)
	default:
		http.NotFound(w, r)
	}
}

// Register mounts the webhook and media routes on mux. With the public key
// unset the routes are STILL registered (so callers see a clean 503 rather
// than a 404) but fail closed; the WARN is logged exactly once here.
func (v *VoiceRoutes) Register(mux *http.ServeMux) {
	if !v.cfg.Enabled {
		v.log.Warn("voice routes disabled: voice.telnyx_public_key is not configured; "+
			"Telnyx webhook and media endpoints return 503 (fail-closed, never signature-optional)",
			"webhook_path", v.WebhookPath(), "media_path", v.MediaPath())
	}
	mux.HandleFunc("POST "+v.WebhookPath(), v.handleWebhook)
	mux.HandleFunc("GET "+v.MediaPath(), v.handleMedia)
}

// failClosed answers 503 for a route whose public key is not configured.
func (v *VoiceRoutes) failClosed(w http.ResponseWriter) {
	http.Error(w, "voice routes disabled: telnyx public key not configured", http.StatusServiceUnavailable)
}

// handleWebhook: signature -> parse -> dispatch. See the Notes section of
// leaf 10 for the 500-mapping choice below.
func (v *VoiceRoutes) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if !v.cfg.Enabled {
		v.failClosed(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		http.Error(w, "unreadable webhook body", http.StatusBadRequest)
		return
	}
	if err := v.verify.Webhook(body, r.Header); err != nil {
		// Missing/mismatched signature: 401 BEFORE any parsing, so an
		// unsigned request can never drive call control.
		v.log.Warn("voice webhook: signature verification failed",
			"error", err, "remote", r.RemoteAddr)
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}
	ev, err := telnyx.ParseCallEvent(body)
	if err != nil {
		// Not a call event we recognise (misrouted webhook): 400. This is a
		// configuration error on the Telnyx side, not a transient failure;
		// redelivery would not change the answer.
		v.log.Warn("voice webhook: unrecognised call event", "error", err)
		http.Error(w, "unrecognised call event", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(ev.EventType, "streaming.") {
		// streaming.started/stopped/failed carry no action for us (the
		// media websocket already knows). Answer 200 WITHOUT touching the
		// manager so Telnyx never redelivers (observed live in uptractor).
		v.log.Debug("voice webhook: streaming lifecycle no-op", "event", ev.EventType)
		writeVoiceOK(w)
		return
	}
	if err := v.mgr.HandleCallEvent(r.Context(), ev); err != nil {
		// 500 = transient: Telnyx redelivers, which is what we want while
		// the failure might clear (Telnyx API unreachable). If live
		// observation shows permanent manager errors (e.g. untracked
		// call.answered) causing redelivery storms, remap those to
		// 200-with-log here - the mapping choice is deliberately recorded
		// per leaf 10 Notes. 500 until observed live.
		v.log.Error("voice webhook: call control error",
			"error", err, "event", ev.EventType, "call_control_id", ev.CallControlID)
		http.Error(w, "call control error", http.StatusInternalServerError)
		return
	}
	v.log.Info("voice webhook: call handled", "event", ev.EventType, "call_control_id", ev.CallControlID)
	writeVoiceOK(w)
}

// writeVoiceOK answers the minimal 200 Telnyx needs (no redelivery).
func writeVoiceOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, "ok"); err != nil {
		// Webhook 200-body write: client (Telnyx) treats any 2xx as
		// delivered; a failed body write after WriteHeader is not
		// actionable.
		return
	}
}

// mediaStartFrame is the Telnyx Media Streams start frame carrying the call
// this socket belongs to plus its media format. The "connected" frame
// ({"event":"connected"}) arrives FIRST and must be skipped: treating it as
// a non-start message dropped every live call into a legacy path in
// uptractor (live drill 2, 2026-10-28).
type mediaStartFrame struct {
	Event string `json:"event"`
	Start struct {
		CallControlID string `json:"call_control_id"`
		MediaFormat   struct {
			Encoding   string `json:"encoding"`
			SampleRate int    `json:"sample_rate"`
		} `json:"media_format"`
	} `json:"start"`
}

// mediaWSConn adapts *websocket.Conn (coder/websocket) to the splice's
// WebSocketConn seam: Read drops the message type (Telnyx media frames are
// text JSON), Write sends text, Close is a normal closure.
type mediaWSConn struct{ c *websocket.Conn }

func (m mediaWSConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := m.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (m mediaWSConn) Write(p []byte) error {
	return m.c.Write(context.Background(), websocket.MessageText, p)
}

func (m mediaWSConn) Close() error {
	return m.c.Close(websocket.StatusNormalClosure, "call ended")
}

// handleMedia: verify socket signature BEFORE the upgrade, read the start
// frame (skip connected, bounded), hand off idempotently through
// Manager.OnMediaStart with the injected SpliceRunner. The stream_url the
// signature covers comes from Manager.StreamURL() - the exact URL leaf 05/11
// passed to streaming_start - never re-derived here.
func (v *VoiceRoutes) handleMedia(w http.ResponseWriter, r *http.Request) {
	if !v.cfg.Enabled {
		v.failClosed(w)
		return
	}
	streamURL := v.mgr.StreamURL()
	if streamURL == "" {
		// Nothing to verify against and no bridge to hand the call to:
		// fail closed rather than accepting an unverifiable socket.
		v.log.Warn("voice media: refused, no stream url configured")
		v.failClosed(w)
		return
	}
	if err := v.verify.Media(r.Header, streamURL); err != nil {
		// 403 BEFORE websocket.Accept: no socket, no upgrade, no parse.
		v.log.Warn("voice media: signature verification failed",
			"error", err, "remote", r.RemoteAddr)
		http.Error(w, "signature verification failed", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Default origin posture: Telnyx dials from its network and sends
		// no Origin header, so no origin allowlisting is needed here. TLS
		// verification is never relaxed anywhere in this flow.
	})
	if err != nil {
		v.log.Error("voice media: accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "call ended")
	ws := mediaWSConn{c: conn}

	start, dtmfCount, err := v.readStartFrame(r.Context(), ws)
	if err != nil {
		v.log.Error("voice media: start frame", "error", err)
		return
	}
	v.log.Info("voice media: start frame",
		"call_control_id", start.CallControlID,
		"encoding", start.Encoding,
		"sample_rate", start.SampleRate,
		"transcode_inbound", start.TranscodeInbound)

	// Handoff is idempotent THROUGH the manager: OnMediaStart runs dial
	// exactly once per ccid (a Telnyx re-send or duplicate start frame
	// cannot spawn two bridges on one socket) and blocks until the call's
	// media path ends under the per-call context.
	err = v.mgr.OnMediaStart(start.CallControlID, func(callCtx context.Context) error {
		return v.cfg.SpliceRunner(callCtx, start.CallControlID, ws, start)
	})
	if err != nil {
		v.log.Error("voice media: splice ended with error",
			"error", err, "call_control_id", start.CallControlID)
	}
	// T7: the DTMF counter is logged once at call end (OnMediaStart has
	// returned, so the splice is done).
	v.log.Info("voice media: call ended",
		"call_control_id", start.CallControlID, "dtmf_frames", dtmfCount)
}

// readStartFrame consumes websocket messages until the start frame arrives
// and returns it with the running DTMF count. The "connected" frame and any
// other non-start events (early media, DTMF) are skipped - DTMF is COUNTED,
// never an error, and never reaches the splice (T7). Bounded: 30s context
// timeout AND 50-frame cap, so a silent or looping socket cannot stall the
// handler goroutine.
func (v *VoiceRoutes) readStartFrame(ctx context.Context, conn WebSocketConn) (StartFrame, int, error) {
	ctx, cancel := context.WithTimeout(ctx, startFrameTimeout)
	defer cancel()
	dtmf := 0
	for i := 0; i < maxStartFrames; i++ {
		data, err := conn.Read(ctx)
		if err != nil {
			return StartFrame{}, dtmf, fmt.Errorf("voice media: read start frame: %w", err)
		}
		var probe struct {
			Event string `json:"event"`
		}
		if jerr := json.Unmarshal(data, &probe); jerr != nil {
			// Undecodable pre-start frame (binary, garbage): skip, counted
			// against the 50-frame bound.
			v.log.Debug("voice media: dropping undecodable pre-start frame")
			continue
		}
		switch probe.Event {
		case "start":
			var frame mediaStartFrame
			if jerr := json.Unmarshal(data, &frame); jerr != nil {
				return StartFrame{}, dtmf, fmt.Errorf("voice media: decode start frame: %w", jerr)
			}
			sf := StartFrame{
				CallControlID: frame.Start.CallControlID,
				Encoding:      frame.Start.MediaFormat.Encoding,
				SampleRate:    frame.Start.MediaFormat.SampleRate,
			}
			if sf.CallControlID == "" {
				return StartFrame{}, dtmf, errors.New("voice media: start frame has empty call_control_id")
			}
			// PCMU transcode flag: the negotiated encoding decides whether
			// the splice transcodes inbound mulaw to L16.
			sf.TranscodeInbound = strings.EqualFold(sf.Encoding, "PCMU")
			return sf, dtmf, nil
		case "dtmf":
			// Counted no-op (T7): never an error, never forwarded.
			dtmf++
			continue
		default:
			// "connected", early media, anything else pre-start: skip.
			continue
		}
	}
	return StartFrame{}, dtmf, fmt.Errorf("voice media: no start frame within %d frames", maxStartFrames)
}
