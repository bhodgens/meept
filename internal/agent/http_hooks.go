// Package agent provides HTTP hook support for external integrations.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// HookAsyncRewakeTopic is the bus topic used when an async hook with
// AsyncRewake=true finishes successfully. Subscribers (typically the
// agent loop) can wake up and react to the completion.
const HookAsyncRewakeTopic = "hook.async_rewake"

// HTTPHookConfig serializes hook configuration.
type HTTPHookConfig struct {
	URL string `json:"url"`
	Method string `json:"method"`
	Headers map[string]string `json:"headers"`
	Timeout time.Duration `json:"timeout"`
	RetryCount int `json:"retry_count"`

	// Async, when true, causes Execute to run in a background goroutine
	// and return immediately. The caller never sees the result error —
	// failures are logged asynchronously. Useful for fire-and-forget
	// integrations where blocking the agent loop would be unacceptable.
	Async bool `json:"async,omitempty"`

	// AsyncRewake, when true (and Async must also be true), publishes a
	// hook.async_rewake bus signal after the async execution completes
	// successfully so the agent loop can wake up and react. Requires
	// SetBus to have been called with a non-nil MessageBus.
	AsyncRewake bool `json:"async_rewake,omitempty"`
}

// HTTPHook implements both SessionStartHook and SessionEndHook interfaces
// for HTTP-based lifecycle integrations.
type HTTPHook struct {
	config  HTTPHookConfig
	client  *http.Client
	logger  *slog.Logger
	allowed []*regexp.Regexp

	// bus is used for the async-rewake signal. Optional; when nil,
	// AsyncRewake is a no-op (logged at warning).
	bus *bus.MessageBus

	// sessionID is included in the rewake payload so subscribers know
	// which session triggered the hook.
	sessionID string

	// hookType labels this hook for rewake payloads (e.g. "session_start",
	// "session_end"). Set by the constructor or callers.
	hookType string

	// wg tracks in-flight async executions so Close/Stop can drain.
	wg sync.WaitGroup

	// mu protects bus and sessionID fields (read in async goroutine).
	mu sync.RWMutex
}

// NewHTTPHook creates a new HTTP hook with the given config and URL allowlist.
func NewHTTPHook(config HTTPHookConfig, allowedURLs []string, logger *slog.Logger) (*HTTPHook, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("HTTP hook requires URL")
	}
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	// Compile allowed URL patterns
	allowed := make([]*regexp.Regexp, 0, len(allowedURLs))
	for _, pattern := range allowedURLs {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid URL allowlist pattern %q: %w", pattern, err)
		}
		allowed = append(allowed, re)
	}

	return &HTTPHook{
		config:  config,
		client:  &http.Client{Timeout: config.Timeout},
		logger:  logger,
		allowed: allowed,
	}, nil
}

// SetBus wires a MessageBus reference for async-rewake signals.
// Nil is safely ignored (defensive nil guard per CLAUDE.md rule).
func (h *HTTPHook) SetBus(b *bus.MessageBus) {
	if b == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bus = b
}

// SetSessionID records the active session ID so it can be included in
// async-rewake bus payloads. Nil-safe (empty string is a valid no-op).
func (h *HTTPHook) SetSessionID(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionID = id
}

// SetHookType labels the hook type for rewake payloads (e.g. "session_start").
func (h *HTTPHook) SetHookType(t string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hookType = t
}

// isURLAllowed checks if the URL matches the allowlist.
func (h *HTTPHook) isURLAllowed(rawURL string) bool {
	if len(h.allowed) == 0 {
		return false // No allowlist = nothing allowed
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	for _, re := range h.allowed {
		if re.MatchString(u.String()) || re.MatchString(u.Host) {
			return true
		}
	}
	return false
}

// Execute sends the HTTP request with the given payload.
//
// When config.Async is true, the request runs in a background goroutine
// and this method returns nil immediately. The caller cannot observe
// async errors; they are logged. When config.AsyncRewake is also true,
// a hook.async_rewake bus signal is published after successful completion.
func (h *HTTPHook) Execute(ctx context.Context, payload any) error {
	if !h.config.Async {
		return h.executeSync(ctx, payload)
	}

	// Async path: snapshot rewake inputs under lock, then fire goroutine.
	h.mu.RLock()
	busRef := h.bus
	sid := h.sessionID
	htype := h.hookType
	h.mu.RUnlock()

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		err := h.executeSync(ctx, payload)
		if err != nil {
			if h.logger != nil {
				h.logger.Warn("async HTTP hook failed",
					"url", h.config.URL,
					"error", err,
				)
			}
			return
		}

		// AsyncRewake: signal the agent loop.
		if h.config.AsyncRewake && busRef != nil {
			rewakePayload := map[string]any{
				"session_id": sid,
				"hook_type":  htype,
				"hook_name":  "http:" + h.config.URL,
			}
			msg, err := models.NewBusMessage(models.MessageTypeEvent, "hook", rewakePayload)
			if err == nil {
				busRef.Publish(HookAsyncRewakeTopic, msg)
				if h.logger != nil {
					h.logger.Debug("async HTTP hook rewake published",
						"topic", HookAsyncRewakeTopic,
						"hook_type", htype,
						"session_id", sid,
					)
				}
			} else if h.logger != nil {
				h.logger.Warn("async rewake: failed to marshal payload", "error", err)
			}
		} else if h.config.AsyncRewake && busRef == nil && h.logger != nil {
			h.logger.Warn("async rewake requested but bus is nil",
				"url", h.config.URL,
			)
		}
	}()

	return nil
}

// executeSync is the synchronous HTTP execution path used by both the
// sync (default) and async (goroutine-wrapped) modes.
func (h *HTTPHook) executeSync(ctx context.Context, payload any) error {
	// Security check
	if !h.isURLAllowed(h.config.URL) {
		return fmt.Errorf("HTTP hook URL %q not in allowlist", h.config.URL)
	}

	// Marshal payload
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, h.config.Method, h.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add headers
	for k, v := range h.config.Headers {
		req.Header.Set(k, v)
	}

	// Execute with retries
	var lastErr error
	for attempt := 0; attempt <= h.config.RetryCount; attempt++ {
		resp, err := h.client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < h.config.RetryCount {
				time.Sleep(time.Second * time.Duration(attempt+1))
				continue
			}
			return fmt.Errorf("HTTP request failed after %d retries: %w", h.config.RetryCount, err)
		}

		// Check response
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errorBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errorBody))
			if attempt < h.config.RetryCount {
				time.Sleep(time.Second * time.Duration(attempt+1))
				continue
			}
			return lastErr
		}

		resp.Body.Close()
		if h.logger != nil {
			h.logger.Debug("HTTP hook executed successfully", "url", h.config.URL, "status", resp.StatusCode)
		}
		return nil
	}

	return lastErr
}

// Wait blocks until all in-flight async executions complete.
// This is primarily intended for graceful shutdown and tests.
func (h *HTTPHook) Wait() {
	h.wg.Wait()
}

// OnSessionStart implements SessionStartHook.
func (h *HTTPHook) OnSessionStart(ctx context.Context, state SessionLifecycleState) ContextTransform {
	h.SetSessionID(state.SessionID)
	h.SetHookType("session_start")
	payload := map[string]any{
		"event":      "session_start",
		"session_id": state.SessionID,
		"agent_id":   state.AgentID,
		"start_time": state.StartTime,
		"metadata":   state.Metadata,
	}
	if err := h.Execute(ctx, payload); err != nil && h.logger != nil {
		h.logger.Warn("HTTP OnSessionStart hook failed",
			"url", h.config.URL,
			"error", err,
		)
	}
	return ContextTransform{}
}

// OnSessionEnd implements SessionEndHook.
func (h *HTTPHook) OnSessionEnd(ctx context.Context, state SessionLifecycleState, result SessionLifecycleResult) error {
	h.SetSessionID(state.SessionID)
	h.SetHookType("session_end")
	payload := map[string]any{
		"event":      "session_end",
		"session_id": state.SessionID,
		"agent_id":   state.AgentID,
		"success":    result.Success,
		"end_time":   result.EndTime,
	}
	if result.Error != nil {
		payload["error"] = result.Error.Error()
	}
	return h.Execute(ctx, payload)
}
