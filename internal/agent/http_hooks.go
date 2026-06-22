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
	"time"
)

// HTTPHookConfig serializes hook configuration.
type HTTPHookConfig struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers"`
	Timeout    time.Duration     `json:"timeout"`
	RetryCount int               `json:"retry_count"`
}

// HTTPHook implements both SessionStartHook and SessionEndHook interfaces
// for HTTP-based lifecycle integrations.
type HTTPHook struct {
	config  HTTPHookConfig
	client  *http.Client
	logger  *slog.Logger
	allowed []*regexp.Regexp
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
func (h *HTTPHook) Execute(ctx context.Context, payload any) error {
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
		defer resp.Body.Close()

		// Check response
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errorBody, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errorBody))
			if attempt < h.config.RetryCount {
				time.Sleep(time.Second * time.Duration(attempt+1))
				continue
			}
			return lastErr
		}

		h.logger.Debug("HTTP hook executed successfully", "url", h.config.URL, "status", resp.StatusCode)
		return nil
	}

	return lastErr
}

// OnSessionStart implements SessionStartHook.
func (h *HTTPHook) OnSessionStart(ctx context.Context, state SessionLifecycleState) (ContextTransform, error) {
	payload := map[string]any{
		"event":      "session_start",
		"session_id": state.SessionID,
		"agent_id":   state.AgentID,
		"start_time": state.StartTime,
		"metadata":   state.Metadata,
	}
	return ContextTransform{}, h.Execute(ctx, payload)
}

// OnSessionEnd implements SessionEndHook.
func (h *HTTPHook) OnSessionEnd(ctx context.Context, state SessionLifecycleState, result SessionLifecycleResult) error {
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
