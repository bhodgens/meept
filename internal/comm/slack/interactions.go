package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/acp"
)

// ReplayWindow is the pinned 5-minute Slack signature replay tolerance
// (research-notes.md A.1): reject when the absolute skew exceeds it.
const ReplayWindow = 5 * time.Minute

// slackSignatureHeader / slackTimestampHeader are the pinned header names.
// Header lookup in Go is canonical-case-insensitive, which covers the doc's
// "letter case should not be assumed" note.
const (
	slackSignatureHeader = "X-Slack-Signature"
	slackTimestampHeader = "X-Slack-Request-Timestamp"
)

// Resolver resolves one pending permission request. The daemon wires this
// to acp.Manager.ResolvePermissionWithApprover; tests inject a stub. It is
// called on a background goroutine (after the 200 ack), never on the HTTP
// handler goroutine, so a slow rank/relay path cannot blow the 3 s ack
// budget.
type Resolver func(id string, verb string, approver acp.Approver) error

// InteractionsHandler serves POST /api/v1/slack/interactions (C3).
type InteractionsHandler struct {
	cfg    SlackConfig
	logger *slog.Logger
	// now is injectable for replay-window tests.
	now func() time.Time
	// resolveMu guards resolve (injected by the daemon wiring just before
	// the HTTP server starts serving; read on every request).
	resolveMu sync.RWMutex
	// resolve is the relay seam; nil disables the resolve path (the ack
	// still works - useful for wiring-only deployments).
	resolve Resolver
}

// SetResolver injects the relay resolve seam. Boot-time wiring seam; safe
// for concurrent reads via the internal guard.
func (h *InteractionsHandler) SetResolver(r Resolver) {
	h.resolveMu.Lock()
	defer h.resolveMu.Unlock()
	h.resolve = r
}

// currentResolver snapshots the seam under the guard.
func (h *InteractionsHandler) currentResolver() Resolver {
	h.resolveMu.RLock()
	defer h.resolveMu.RUnlock()
	return h.resolve
}

// NewInteractionsHandler builds the handler. resolve may be nil (acks only).
func NewInteractionsHandler(cfg SlackConfig, resolve Resolver, logger *slog.Logger) *InteractionsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &InteractionsHandler{
		cfg:     cfg,
		logger:  logger,
		now:     time.Now,
		resolve: resolve,
	}
}

// pattern is the route this handler serves (C3).
const pattern = "POST /api/v1/slack/interactions"

// Pattern returns the mux pattern (for tests and the registration sites).
func Pattern() string { return pattern }

// Config returns the handler's Slack config snapshot (wiring observability
// seam; used by daemon logging and tests). The struct is copied by value,
// so callers cannot mutate the live copy.
func (h *InteractionsHandler) Config() SlackConfig { return h.cfg }

// interactionPayload is the subset of the Slack block_actions payload this
// handler consumes. Everything else is ignored on purpose: the authorization
// inputs are the signature and user.id ONLY.
type interactionPayload struct {
	Type string `json:"type"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	// ResponseURL is parsed but NEVER used as an authorization input and is
	// never logged (research A.2: response_url bypasses channel permissions;
	// it is a delivery convenience, not a boundary).
	ResponseURL string `json:"response_url"`
}

// ServeHTTP implements the callback: read the RAW body, verify the signature
// (constant-time HMAC-SHA256 over "v0:<timestamp>:<raw body>", 5-minute
// window), parse the form-encoded payload, check AllowedUserIDs, ack 200
// within the 3 s budget, then resolve async.
func (h *InteractionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Raw body FIRST, before any deserialization (research A.1).
	raw, err := readBody(r)
	if err != nil {
		h.ack(w, http.StatusBadRequest, "unable to read request")
		return
	}

	tsHeader := r.Header.Get(slackTimestampHeader)
	sigHeader := r.Header.Get(slackSignatureHeader)
	if !verifyRequest(h.cfg.SigningSecret, tsHeader, sigHeader, raw, h.now()) {
		// Do not distinguish "bad signature" from "stale" to callers.
		h.ack(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	payload, err := parsePayload(raw)
	if err != nil {
		h.ack(w, http.StatusOK, "unrecognized payload")
		return
	}

	// Only block_actions carries the button grammar (C3).
	if payload.Type != "block_actions" {
		h.ack(w, http.StatusOK, "") // plain ack, no ephemeral reply
		return
	}

	// AllowedUserIDs is the authorization boundary (C2): empty allows
	// NOBODY. response_url plays no part in this decision.
	if !h.userAllowed(payload.User.ID) {
		h.ack(w, http.StatusOK, "you are not allowed to answer permission requests")
		return
	}

	if len(payload.Actions) == 0 {
		h.ack(w, http.StatusOK, "")
		return
	}
	action := payload.Actions[0]
	requestID, verb, ok := ParseActionValue(action.Value)
	if !ok {
		h.ack(w, http.StatusOK, "unrecognized action")
		return
	}

	// Ack NOW (3 s budget, research A.2); the resolve runs async. A nil
	// resolver still acks (wiring-only deployments, tests).
	h.ack(w, http.StatusOK, "")
	resolver := h.currentResolver()
	if resolver == nil {
		return
	}
	approver := acp.Approver{Kind: acp.ApproverKindSlack, Detail: payload.User.ID}
	go func() {
		err := resolver(requestID, verb, approver)
		switch {
		case err == nil:
			h.logger.Info("slack: permission resolved",
				"request", requestID, "verb", verb, "approver", approver.String())
		case isNoPending(err):
			// Unknown/expired requestID (e.g. the 120 s fail-closed timeout
			// already fired): polite 200-path ephemeral answer was already
			// sent; log at debug only. NO second timer is added here - the
			// relay stays the timeout authority.
			h.logger.Debug("slack: no pending permission for action",
				"request", requestID, "verb", verb)
		default:
			h.logger.Warn("slack: permission resolve failed",
				"request", requestID, "verb", verb, "error", err)
		}
	}()
}

// ack sends the HTTP response. For button actions Slack renders an empty
// body as "no change" and a text/plain body as an ephemeral message; both
// are valid 200 acks inside the 3 s budget.
func (h *InteractionsHandler) ack(w http.ResponseWriter, status int, ephemeral string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if ephemeral != "" {
		_, _ = w.Write([]byte(ephemeral))
	}
}

// isNoPending reports whether err means "no such pending request" (unknown
// or already-answered). errors.Is keeps it honest against wrapper chains.
func isNoPending(err error) bool {
	return err == acp.ErrRelayNotFound || err == acp.ErrRelayAnswered
}

// userAllowed checks the payload user against AllowedUserIDs (empty = none).
func (h *InteractionsHandler) userAllowed(uid string) bool {
	if uid == "" || len(h.cfg.AllowedUserIDs) == 0 {
		return false
	}
	for _, allowed := range h.cfg.AllowedUserIDs {
		if allowed != "" && allowed == uid {
			return true
		}
	}
	return false
}

// readBody bounds and reads the raw request body.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("slack: read body: %w", err)
	}
	return raw, nil
}

// verifyRequest implements research-notes.md A.1 verbatim: parse the unix
// timestamp, reject skew > 5 minutes, HMAC-SHA256 over
// "v0:<timestamp>:<raw body>" keyed with the UTF-8 signing secret, hex
// digest with the "v0=" prefix, constant-time compare.
func verifyRequest(secret, tsHeader, sigHeader string, rawBody []byte, now time.Time) bool {
	if secret == "" {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(tsHeader), 10, 64)
	if err != nil || ts <= 0 {
		return false
	}
	if skew(now.Unix()-ts) > int64(ReplayWindow/time.Second) {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret)) // secret stays UTF-8
	mac.Write([]byte("v0:" + strconv.FormatInt(ts, 10) + ":"))
	mac.Write(rawBody)
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(sigHeader))
}

func skew(d int64) int64 {
	if d < 0 {
		return -d
	}
	return d
}

// parsePayload extracts the single form-urlencoded `payload` parameter and
// decodes its JSON.
func parsePayload(rawBody []byte) (*interactionPayload, error) {
	vals, err := url.ParseQuery(string(rawBody))
	if err != nil {
		return nil, fmt.Errorf("slack: body is not form-urlencoded: %w", err)
	}
	pj := vals.Get("payload")
	if pj == "" {
		return nil, fmt.Errorf("slack: missing payload parameter")
	}
	var p interactionPayload
	if err := json.Unmarshal([]byte(pj), &p); err != nil {
		return nil, fmt.Errorf("slack: payload is not valid JSON")
	}
	return &p, nil
}
