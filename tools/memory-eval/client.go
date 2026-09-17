package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// chatClient is a minimal OpenAI-compatible /chat/completions client with a
// single retry on transport error. No grammar constraint: production ambient
// extraction and distillation are currently unconstrained, so the eval grades
// the model's native ability to emit parseable JSON.
type chatClient struct {
	endpoint string
	model    string
	timeout  time.Duration
	http     *http.Client
}

func newChatClient(endpoint, model string, timeout time.Duration) *chatClient {
	return &chatClient{
		endpoint: endpoint,
		model:    model,
		timeout:  timeout,
		http:     &http.Client{Timeout: timeout},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// chat sends one completion request and returns (content, latencyMs, tokensUsed, err).
func (c *chatClient) chat(ctx context.Context, system, user string) (string, float64, int, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: 0.2,
		MaxTokens:   1024,
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", 0, 0, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.endpoint+"/chat/completions", bytes.NewReader(reqBody))
		if err != nil {
			return "", 0, 0, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("transport: %w", err)
			continue // retry once on transport error
		}
		latency := float64(time.Since(start).Milliseconds())
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read body: %w", readErr)
			continue
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("close body: %w", closeErr)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
			continue
		}
		var cr chatResponse
		if err := json.Unmarshal(body, &cr); err != nil {
			return "", latency, 0, fmt.Errorf("decode response: %w", err)
		}
		if len(cr.Choices) == 0 {
			return "", latency, 0, fmt.Errorf("no choices in response")
		}
		return cr.Choices[0].Message.Content, latency,
			cr.Usage.PromptTokens + cr.Usage.CompletionTokens, nil
	}
	return "", 0, 0, lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
