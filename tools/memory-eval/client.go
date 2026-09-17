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
	// grammar, when non-empty, is attached to the request payload as the
	// llama.cpp wire "grammar" field — measuring the constrained path.
	grammar string
	http    *http.Client
}

func newChatClient(endpoint, model string, timeout time.Duration) *chatClient {
	return &chatClient{
		endpoint: endpoint,
		model:    model,
		timeout:  timeout,
		http:     &http.Client{Timeout: timeout},
	}
}

// setGrammar enables grammar-constrained requests (llama.cpp endpoints only).
func (c *chatClient) setGrammar(g string) { c.grammar = g }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Grammar     string        `json:"grammar,omitempty"`
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
		Grammar:     c.grammar,
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	lastLatency := 0.0
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
			// F30: even a failed attempt costs time — report the real
			// elapsed instead of 0 so mean latency stays honest.
			lastErr = fmt.Errorf("transport: %w", err)
			lastLatency = float64(time.Since(start).Milliseconds())
			continue // retry once on transport error
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		// F30: latency measures the completed body (ReadAll + Close), not
		// just the response headers.
		latency := float64(time.Since(start).Milliseconds())
		if readErr != nil {
			lastErr = fmt.Errorf("read body: %w", readErr)
			lastLatency = latency
			continue
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("close body: %w", closeErr)
			lastLatency = latency
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
			lastLatency = latency
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
	// F30: retries keep last-attempt semantics — return the last attempt's
	// error with its real elapsed time.
	return "", lastLatency, 0, lastErr
}

// chatJudge sends the judge-lane request: temperature 0, max_tokens 4, and NO
// grammar constraint (the judge answers a bare yes/no). It posts to whatever
// endpoint the client carries, so a --judge-endpoint client is used as-is.
func (c *chatClient) chatJudge(ctx context.Context, system, user string) (string, float64, int, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature: 0,
		MaxTokens:   4,
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal judge request: %w", err)
	}

	var lastErr error
	lastLatency := 0.0
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
			return "", 0, 0, fmt.Errorf("build judge request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("transport: %w", err)
			lastLatency = float64(time.Since(start).Milliseconds())
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		// F30: latency measures the completed body (ReadAll + Close), not
		// just the response headers.
		latency := float64(time.Since(start).Milliseconds())
		if readErr != nil {
			lastErr = fmt.Errorf("read body: %w", readErr)
			lastLatency = latency
			continue
		}
		if closeErr != nil {
			lastErr = fmt.Errorf("close body: %w", closeErr)
			lastLatency = latency
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
			lastLatency = latency
			continue
		}
		var cr chatResponse
		if err := json.Unmarshal(body, &cr); err != nil {
			return "", latency, 0, fmt.Errorf("decode judge response: %w", err)
		}
		if len(cr.Choices) == 0 {
			return "", latency, 0, fmt.Errorf("no choices in judge response")
		}
		return cr.Choices[0].Message.Content, latency,
			cr.Usage.PromptTokens + cr.Usage.CompletionTokens, nil
	}
	// F30: retries keep last-attempt semantics — return the last attempt's
	// error with its real elapsed time.
	return "", lastLatency, 0, lastErr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
