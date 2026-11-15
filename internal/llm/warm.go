package llm

// Context-warm seam (phase2-context-warm leaf 01, root Contract C4).
//
// Warm pre-issues a throwaway chat-completion request whose ONLY purpose is
// to plant the turn's prompt prefix in the provider-side prompt cache (and,
// for llama.cpp-family servers, the KV slot reached under the session
// affinity scope), so the real turn that follows pays decode, not prefill.
//
// Prefix identity is the whole point: the request is built through the SAME
// buildChatRequest serialization path a real turn uses (system-prompt cache
// boundary stripping, ToOpenAIDictWithStore message rendering, stable tool
// order, prompt_cache_key), with max_tokens forced to 1 and streaming off.
// The completion text is discarded and NEVER logged.
//
// Error posture (R8): any failure — transport, quota *QuotaResetError, 4xx,
// timeout, panic — is logged at DEBUG with the alias + reason and swallowed.
// Warm participates in NONE of the loop machinery: no short-retry budget, no
// recordAliasFailure, no endpoint/alias block arming, no budget-gate
// consumption, no quota parking, no cache put, no llm_calls turn row, no
// metrics.Store request record, no tier/degradation signal, no WS relay, no
// session message. It never returns an error by design.
//
// Concurrency: one dedicated in-flight lane per Client (cap 1). A new Warm
// REPLACES the in-flight one (cancels the previous request — replace-discard)
// rather than queueing behind it, and a warm never queues behind the real
// turn's slot gate either (it rides its own transport path). A hung warm is
// bounded by an internal ceiling deadline wrapping the caller ctx, so the
// lane is always released.
//
// Design notes for the orchestrator:
//   - The C4 sketch says []Message; the codebase's real message type is
//     ChatMessage, so the arg type is []ChatMessage (semantics unchanged).
//   - `alias` is a caller-supplied label used for DEBUG attribution only: a
//     bare *Client serves whatever model its (possibly Reconfigured) config
//     holds; request-scoped alias resolution is the Resolver/ProviderManager
//     lane and is NOT reachable from here. Leaf 02 should pass the alias it
//     will use for the real turn so logs correlate.
//   - The lane state lives in a package-level side table keyed by *Client
//     rather than new Client struct fields because this leaf may only touch
//     warm.go (concurrency notice). If the integration commit owns
//     client.go, move the two fields onto Client and drop the sync.Map.
//   - The HTTP dispatch bypasses doRequest on purpose: doRequest owns the
//     slot gate, the pacer, the Error-level 400 log, and the metricsStore
//     error/success records. Warm reuses only the header plumbing
//     (applyExtraHeaders + applyAffinityHeader) and the auth resolution
//     shape, so it cannot write a metrics row or surface a non-DEBUG log.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// warmMaxTokens is the forced completion cap of a warm request: one token is
// enough for the server to prefill (and cache) the whole prompt.
const warmMaxTokens = 1

// warmCeilingDefault is the hard internal deadline a warm request carries on
// top of the caller ctx, so a hung upstream can never pin the warm lane.
const warmCeilingDefault = 10 * time.Second

// warmRequestCeiling is the effective ceiling (test seam: warm_test.go
// shrinks it to exercise lane release without slow tests).
var warmRequestCeiling = warmCeilingDefault

// warmLane is the per-client in-flight warm slot (cap 1, replace-discard).
type warmLane struct {
	mu     sync.Mutex
	active *warmHandle
}

// warmHandle owns the cancel func of the currently in-flight warm request.
type warmHandle struct {
	cancel context.CancelFunc
}

// warmLanes maps *Client -> *warmLane. Clients are long-lived provider
// instances (one per registered model/provider), so entries never need
// eviction; see the file doc comment for why this is not a Client field.
var warmLanes sync.Map // *Client -> *warmLane

// lane returns the client's warm lane, creating it on first use.
func (c *Client) lane() *warmLane {
	v, _ := warmLanes.LoadOrStore(c, &warmLane{})
	return v.(*warmLane)
}

// Warm pre-issues a cache-warming request for the given alias and rendered
// messages: non-streaming, max_tokens=1, same session-affinity scope as the
// real turn. It NEVER returns a completion, NEVER consumes turn budget,
// NEVER starts tools or an agent turn. Failures (any kind) are logged at
// DEBUG and swallowed by design.
//
// Fire-and-forget: Warm returns immediately; the request runs on an internal
// goroutine bound to a ceiling-wrapped copy of ctx. Cancelling ctx (or
// calling Warm again) discards the in-flight warm.
//
// sessionScope is the turn's ACCOUNTING session id — the SAME value the real
// turn stamps via WithTaskScope — so prompt_cache_key and the
// llm.session_affinity header land on the same provider-side lane. An empty
// scope sends neither (client rule: never an empty header/key value).
func (c *Client) Warm(ctx context.Context, alias string, messages []ChatMessage, sessionScope string) {
	if c == nil || len(messages) == 0 {
		return
	}

	ceiling := warmRequestCeiling
	if ceiling <= 0 {
		ceiling = warmCeilingDefault
	}
	reqCtx, cancel := context.WithTimeout(ctx, ceiling)

	lane := c.lane()
	h := &warmHandle{cancel: cancel}

	// Replace-discard: the newest warm wins; the previous in-flight one is
	// cancelled outright instead of queueing.
	lane.mu.Lock()
	prev := lane.active
	lane.active = h
	lane.mu.Unlock()
	if prev != nil {
		prev.cancel()
	}

	go func() {
		// Panic-safe lane release: whatever dispatchWarm does (or fails to
		// do), the lane must not stay pinned.
		defer func() {
			if r := recover(); r != nil {
				c.logger.Debug("Warm request panicked", "alias", alias, "panic", r)
			}
			lane.mu.Lock()
			if lane.active == h {
				lane.active = nil
			}
			lane.mu.Unlock()
			cancel()
		}()
		c.dispatchWarm(reqCtx, alias, messages, sessionScope)
	}()
}

// dispatchWarm builds the payload through the shared turn serializer and
// issues it on the warm-only transport path. All failures end in one DEBUG
// line naming alias + reason.
func (c *Client) dispatchWarm(ctx context.Context, alias string, messages []ChatMessage, sessionScope string) {
	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()
	if cfg == nil || cfg.BaseURL == "" {
		c.logger.Debug("Warm skipped: no serving config", "alias", alias)
		return
	}

	// SAME serialization path as a real turn (prefix identity): message
	// rendering, cache-boundary strip, model stamping, prompt_cache_key via
	// the chatOptions.sessionID lane (client.go buildChatRequest). No tools,
	// no grammar, no reasoning, no prefill — a warm is a bare prefix echo.
	opts := []ChatOption{
		WithMaxTokens(warmMaxTokens),
		WithTaskScope("", sessionScope),
	}
	_, payload, err := c.buildChatRequest(messages, cfg, opts, false)
	if err == nil {
		// The only intentional payload deltas from a real Chat turn:
		// max_tokens=1 (forced above) and an explicit non-streaming flag.
		payload["stream"] = false
		err = c.sendWarmRequest(ctx, cfg, payload, sessionScope)
	}
	if err != nil {
		c.logger.Debug("Warm request failed (swallowed by design)",
			"alias", alias, "reason", err.Error())
		return
	}
	c.logger.Debug("Warm request completed", "alias", alias)
}

// sendWarmRequest is the warm-only transport: the same URL construction,
// auth, extra-header and affinity-header plumbing as doRequest, minus the
// slot gate, the pacer, the retry classification, the error-level 400 log,
// and every metrics write.
func (c *Client) sendWarmRequest(ctx context.Context, cfg *ModelConfig, payload map[string]any, sessionScope string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	url := baseURL + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Auth resolution mirrors doRequest (OAuth token when configured, else
	// the static bearer key) so a warm reaches the same backend the real
	// turn will hit.
	if c.tokenResolver != nil && c.oauthProvider != "" {
		token, terr := c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
		if terr != nil {
			return fmt.Errorf("resolve oauth token: %w", terr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	// Same header lane as a real turn: extras first (with the
	// ${session_id} sentinel), then the default affinity header (never
	// empty, extras win per key — applyAffinityHeader owns that rule).
	c.applyExtraHeaders(req, cfg, sessionScope)
	c.applyAffinityHeader(req, sessionScope)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("egress: %w", err)
	}
	defer func() {
		// Drain a bounded slice so the connection can be reused, then close.
		// A drain or close failure here is benign on a bounded read, and Warm
		// is silent-failure by contract C4: DEBUG only, never alters behavior.
		if _, cerr := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)); cerr != nil {
			c.logger.Debug("Warm body drain failed (benign)", "err", cerr.Error())
		}
		if cerr := resp.Body.Close(); cerr != nil {
			c.logger.Debug("Warm body close failed (benign)", "err", cerr.Error())
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// Status only — the body of a failed warm is never worth a log
		// line, and completion text is never logged at any level.
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return nil
}

// Warm on the Anthropic client class: explicit no-op with a DEBUG line.
// Voice turns ride the OpenAI-compatible local endpoint, so warming
// Anthropic is out of scope for this tree (phase2 leaf 01, task 4); the
// method exists so leaf 02 can call the seam on any Chatter-shaped client
// without knowing which class resolved.
func (c *AnthropicClient) Warm(_ context.Context, alias string, _ []ChatMessage, _ string) {
	if c == nil {
		return
	}
	c.logger.Debug("Warm unsupported for Anthropic client class (no-op)", "alias", alias)
}

// Warm on the Codex client class: explicit no-op with a DEBUG line (same
// rationale as the Anthropic twin above).
func (c *CodexClient) Warm(_ context.Context, alias string, _ []ChatMessage, _ string) {
	if c == nil {
		return
	}
	c.logger.Debug("Warm unsupported for Codex client class (no-op)", "alias", alias)
}
