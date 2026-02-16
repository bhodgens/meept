package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout        = 120 * time.Second
	maxRetries            = 3
	retryBackoffBase      = 2.0 // seconds - exponential: 2, 4, 8
)

// HTTP status codes that warrant a retry
var retryableStatusCodes = map[int]bool{
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// Error types

// APIError is returned when the remote API returns an error response.
type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Detail)
}

// ClientError is the base error for LLM client errors.
type ClientError struct {
	Message string
	Cause   error
}

func (e *ClientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ClientError) Unwrap() error {
	return e.Cause
}

// Client is an HTTP client for OpenAI-compatible chat completions endpoints.
type Client struct {
	config     *ModelConfig
	budget     *Budget
	httpClient *http.Client
	logger     *slog.Logger
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// WithBudget sets the token budget for the client.
func WithBudget(budget *Budget) ClientOption {
	return func(c *Client) {
		c.budget = budget
	}
}

// WithLogger sets the logger for the client.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// WithTimeout sets the HTTP timeout for the client.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// NewClient creates a new LLM client.
func NewClient(config *ModelConfig, opts ...ClientOption) *Client {
	c := &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Chat sends a chat completion request and returns the parsed response.
func (c *Client) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	// Apply chat options
	chatOpts := &chatOptions{
		temperature: c.config.Temperature,
		maxTokens:   c.config.MaxTokens,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Budget gate
	if c.budget != nil {
		if !c.budget.CheckBudget() {
			return nil, &BudgetExceededError{Message: "Token budget exceeded - request blocked"}
		}
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Build request payload
	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgDicts[i] = msg.ToOpenAIDict()
	}

	payload := map[string]any{
		"model":       c.config.ModelID,
		"messages":    msgDicts,
		"temperature": chatOpts.temperature,
		"max_tokens":  chatOpts.maxTokens,
	}

	if len(chatOpts.tools) > 0 {
		payload["tools"] = chatOpts.tools
	}

	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := c.doRequest(ctx, payload)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && retryableStatusCodes[apiErr.StatusCode] {
				c.logger.Warn("Retryable error",
					"status", apiErr.StatusCode,
					"attempt", attempt,
					"max_retries", maxRetries,
				)
				lastErr = err
				if attempt < maxRetries {
					sleepDuration := time.Duration(retryBackoffBase*float64(attempt)) * time.Second
					select {
					case <-time.After(sleepDuration):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				continue
			}
			return nil, err
		}

		// Record usage
		if c.budget != nil {
			c.budget.RecordUsage(resp.Usage)
		}

		return resp, nil
	}

	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", maxRetries),
		Cause:   lastErr,
	}
}

// chatOptions holds options for a chat request.
type chatOptions struct {
	tools       []ToolDefinition
	temperature float64
	maxTokens   int
}

// ChatOption is a functional option for configuring a chat request.
type ChatOption func(*chatOptions)

// WithTools sets the tools for the chat request.
func WithTools(tools []ToolDefinition) ChatOption {
	return func(o *chatOptions) {
		o.tools = tools
	}
}

// WithTemperature sets the temperature for the chat request.
func WithTemperature(temp float64) ChatOption {
	return func(o *chatOptions) {
		o.temperature = temp
	}
}

// WithMaxTokens sets the max tokens for the chat request.
func WithMaxTokens(tokens int) ChatOption {
	return func(o *chatOptions) {
		o.maxTokens = tokens
	}
}

// doRequest performs the HTTP request and parses the response.
func (c *Client) doRequest(ctx context.Context, payload map[string]any) (*Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	url := strings.TrimSuffix(c.config.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{Message: "failed to read response", Cause: err}
	}

	// Check for retryable status codes
	if retryableStatusCodes[resp.StatusCode] {
		detail := string(respBody)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Check for other error status codes
	if resp.StatusCode != http.StatusOK {
		detail := string(respBody)
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Parse response
	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, &ClientError{Message: "failed to parse response", Cause: err}
	}

	return c.parseResponse(&chatResp)
}

// parseResponse converts a raw ChatResponse to a Response.
func (c *Client) parseResponse(chatResp *ChatResponse) (*Response, error) {
	if len(chatResp.Choices) == 0 {
		return nil, &ClientError{Message: "no choices in response"}
	}

	choice := chatResp.Choices[0]
	msg := choice.Message

	var content string
	if msg.Content != nil {
		content = *msg.Content
	}

	var toolCalls []ToolCall
	if len(msg.ToolCalls) > 0 {
		toolCalls = make([]ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			toolCalls[i] = tc.ToToolCall()
		}
	}

	model := chatResp.Model
	if model == "" {
		model = c.config.ModelID
	}

	return &Response{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage: TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
		},
		Model:        model,
		FinishReason: choice.FinishReason,
	}, nil
}

// SwitchModel switches to a different model/endpoint at runtime.
func (c *Client) SwitchModel(config *ModelConfig) {
	c.config = config
	c.logger.Info("Switched model",
		"model", config.ModelID,
		"base_url", config.BaseURL,
	)
}

// Close closes the client (releases resources).
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Config returns the current model configuration.
func (c *Client) Config() *ModelConfig {
	return c.config
}
