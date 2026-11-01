package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/services"
)

// SlackChannel publishes pending permission requests as interactive Slack
// messages (services.PushChannel) and owns the acp.PermissionNotifier
// adapter the daemon wires into the relay lane.
//
// Threading: the notifier methods run on the relay's permission goroutine
// with NO session lock held and MUST NOT block (mutexio rule,
// relay.go:85-98). Publishing is therefore handed to a per-call goroutine
// guarded by a WaitGroup so tests (and a graceful shutdown) can wait for
// in-flight HTTP posts; the notifier call itself returns within
// microseconds.
type SlackChannel struct {
	cfg    SlackConfig
	logger *slog.Logger
	client *http.Client

	mu       sync.Mutex
	inflight sync.WaitGroup
	// cardTS maps requestID -> (channel, ts) of the posted approval card so
	// NotifyPermissionResolved can chat.update it. Bounded by dropping the
	// oldest entry past maxTrackedCards (a permission card that was never
	// resolved within this many subsequent requests loses its flip update -
	// cosmetic only, the relay is unaffected).
	cardTS map[string]cardRef
}

const maxTrackedCards = 64

type cardRef struct {
	channel string
	ts      string
}

// Compile-time seams.
var (
	_ services.PushChannel   = (*SlackChannel)(nil)
	_ acp.PermissionNotifier = (*SlackChannel)(nil)
)

// NewSlackChannel builds the channel (C1). Callers must have passed
// cfg.Validate(); construction itself does not fail on an unusable token
// because every failure mode surfaces per-call from the API response.
func NewSlackChannel(cfg SlackConfig, logger *slog.Logger) *SlackChannel {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlackChannel{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 10 * time.Second},
		cardTS: make(map[string]cardRef),
	}
}

// CanReceive: the channel is broadcast-by-config (a fixed ChannelID), so it
// accepts alert-type pushes; the permission lane does NOT ride Push - it
// rides the Notifier adapter (NotifyPermissionRequested) so the interactive
// card carries the relay's requestID, which a generic PushMessage cannot.
func (c *SlackChannel) CanReceive(ctx context.Context, sessionID string, msg *services.PushMessage) bool {
	if msg == nil {
		return false
	}
	return msg.Type == services.PushTypeAlert
}

// Push delivers a plain push message to the configured channel. Best-effort
// like the other PushChannel implementations: errors are returned for the
// registry to log.
func (c *SlackChannel) Push(ctx context.Context, msg *services.PushMessage) error {
	if msg == nil {
		return nil
	}
	card := resolveCard{
		Channel: c.cfg.ChannelID,
		Text:    msg.Content,
		Blocks: []block{
			{Type: "section", Text: &textObj{Type: "mrkdwn", Text: escapeSlackText(msg.Content)}},
		},
	}
	return c.postChat(ctx, "chat.postMessage", card, nil)
}

// Notifier returns the acp.PermissionNotifier adapter (C1): the daemon
// passes this to the relay lane when the channel is enabled.
func (c *SlackChannel) Notifier() acp.PermissionNotifier { return c }

// Wait blocks until all in-flight publish goroutines have finished. Test
// and shutdown seam - never called on the notifier path.
func (c *SlackChannel) Wait() { c.inflight.Wait() }

// NotifyPermissionRequested posts the interactive approval card. NON-BLOCKING
// per the PermissionNotifier contract: the HTTP call runs on a goroutine.
// Returns true (listener present) whenever the channel is wired - the
// relay's own "no listener still waits" semantics are upstream of us.
func (c *SlackChannel) NotifyPermissionRequested(p acp.PendingPermission) bool {
	if c == nil || !c.cfg.Enabled {
		return false
	}
	requestID := p.ID
	if requestID == "" {
		return false
	}
	summary := p.Summary
	if summary == "" && len(p.Params) > 0 {
		summary = truncateRunes(string(p.Params), 300)
	}
	card := renderRequestCard(c.cfg.ChannelID, requestID, summary, p.AgentID)

	c.inflight.Add(1)
	go func() {
		defer c.inflight.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var ts string
		err := c.postChat(ctx, "chat.postMessage", card, func(resp chatAPIResponse) {
			ts = resp.TS
		})
		if err != nil {
			// Never log the token; the error text is from the API wrapper.
			c.logger.Warn("slack: failed to post permission card", "request", requestID, "error", err)
			return
		}
		c.mu.Lock()
		if len(c.cardTS) >= maxTrackedCards {
			c.dropOldestCardLocked()
		}
		c.cardTS[requestID] = cardRef{channel: c.cfg.ChannelID, ts: ts}
		c.mu.Unlock()
	}()
	return true
}

// NotifyPermissionResolved flips the posted card to its terminal state via
// chat.update (best-effort, async, non-blocking).
func (c *SlackChannel) NotifyPermissionResolved(p acp.PendingPermission, decision, reason string, approver acp.Approver) {
	if c == nil || !c.cfg.Enabled {
		return
	}
	c.mu.Lock()
	ref, ok := c.cardTS[p.ID]
	delete(c.cardTS, p.ID)
	c.mu.Unlock()
	if !ok {
		return
	}
	card := renderResolvedCard(ref.channel, ref.ts, decision, approver.String())

	c.inflight.Add(1)
	go func() {
		defer c.inflight.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.postChat(ctx, "chat.update", card, nil); err != nil {
			c.logger.Warn("slack: failed to update permission card", "request", p.ID, "error", err)
		}
	}()
}

// dropOldestCardLocked evicts an arbitrary (Go map order) entry; the map is
// only a best-effort card-flip cache, so no LRU bookkeeping is warranted.
// Caller holds c.mu.
func (c *SlackChannel) dropOldestCardLocked() {
	for k := range c.cardTS {
		delete(c.cardTS, k)
		break
	}
}

// chatAPIResponse is the common Slack Web API envelope. ok=false carries
// error; chat.postMessage/chat.update success carries ts.
type chatAPIResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	TS    string `json:"ts,omitempty"`
}

// postChat posts one Web API call. Body bytes are built here so the token
// never appears in any error path; the response hook runs before the error
// check (a JSON envelope with ok=false still parses).
func (c *SlackChannel) postChat(ctx context.Context, method string, card resolveCard, onResp func(chatAPIResponse)) error {
	body, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("slack: encode %s: %w", method, err)
	}
	url := c.cfg.apiURL() + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+c.cfg.BotToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("slack: %s: read response: %w", method, err)
	}
	var env chatAPIResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("slack: %s: unexpected response (status %d)", method, resp.StatusCode)
	}
	if onResp != nil {
		onResp(env)
	}
	if !env.OK {
		return fmt.Errorf("slack: %s: api error: %s", method, env.Error)
	}
	return nil
}

// truncateRunes moved to text.go alongside escapeSlackText.
