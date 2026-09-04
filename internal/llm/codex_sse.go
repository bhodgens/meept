package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// This file implements Responses-API SSE streaming for CodexClient, closing
// the previously documented stream:false drift.
//
// Upstream contract (openai/codex, codex-rs 0.153.x):
//   - codex-rs/codex-api/src/endpoint/responses.rs — every /responses POST is
//     a stream: it inserts Accept: text/event-stream on the request and spawns
//     an SSE event loop; the ResponsesApiRequest wire body carries stream:true.
//   - codex-rs/codex-api/src/sse/responses.rs (process_responses_event) — the
//     SSE event grammar consumed here:
//
//	response.created                       → lifecycle start (ignored)
//	response.output_item.added             → item start (ignored)
//	response.output_text.delta             → {delta} assistant text chunk
//	response.reasoning_summary_text.delta  → {delta} reasoning chunk
//	response.output_item.done              → {item} full output item:
//	    message (content[].output_text), reasoning (summary[].summary_text),
//	    function_call (call_id/name/arguments) → ToolCall
//	response.function_call_arguments.delta → argument chunk (item_id-keyed)
//	response.completed                     → TERMINAL: {response:{id,usage,…}}
//	    carries the token usage
//	response.failed                        → terminal error: {response:{error}}
//	response.incomplete                    → terminal error:
//	    {response:{incomplete_details:{reason}}}
//
//   - codex-rs/codex-api/tests/clients.rs — pins Accept: text/event-stream and
//     stream:true on the wire for every streamed request.
//   - process_sse: upstream treats a stream that ENDS without response.completed
//     as an error ("stream closed before response.completed"); so do we.

// codexResponsesStreamRequest is the JSON shape of the "stream" request field
// for ChatGPT-backed Responses requests. It serializes as a bare true/false
// today but is kept flexible so a future object-form (stream_options) needs
// no wire-shape migration.
type codexResponsesStreamRequest struct {
	Enabled bool
}

// MarshalJSON renders the stream field. Responses API accepts a boolean;
// codex-rs always sends `stream: true`.
func (s codexResponsesStreamRequest) MarshalJSON() ([]byte, error) {
	if s.Enabled {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}

// UnmarshalJSON accepts the boolean form (and tolerates null); the object
// form is accepted-but-discarded so a server echo never breaks parsing.
func (s *codexResponsesStreamRequest) UnmarshalJSON(b []byte) error {
	var enabled bool
	if err := json.Unmarshal(b, &enabled); err == nil {
		s.Enabled = enabled
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err == nil {
		s.Enabled = true
		return nil
	}
	return fmt.Errorf("stream: unsupported shape %s", b)
}

// codexResponsesItem is the subset of a Responses output item (or an
// SSE response.output_item.done payload) that we consume. The same shape
// is used for non-streaming output[] entries and streamed item events.
type codexResponsesItem struct {
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Summary []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// codexStreamAccumulator collects the folded stream state. Content and
// reasoning have TWO channels, mirroring upstream semantics: output_item.done
// items are authoritative (codex-rs builds the turn transcript from them),
// while *.delta events are the live-render channel and serve as the fallback
// when a deployment emits deltas without corresponding item events. Final
// assembly prefers the item channel and falls back to the delta channel.
// parseResponsesItem folds one output item into it, mirroring the
// non-streaming semantics of (*CodexClient).parseResponse: message →
// content, reasoning → summary, function_call → ToolCall.
type codexStreamAccumulator struct {
	itemContent    strings.Builder
	itemReasoning  strings.Builder
	deltaContent   strings.Builder
	deltaReasoning strings.Builder
	toolCalls      []ToolCall
	usage          TokenUsage
}

func parseResponsesItem(item codexResponsesItem, acc *codexStreamAccumulator) {
	switch item.Type {
	case "message":
		for _, part := range item.Content {
			if part.Type == "output_text" && part.Text != "" {
				acc.itemContent.WriteString(part.Text)
			}
		}
	case "reasoning":
		for _, part := range item.Summary {
			if part.Type == "summary_text" && part.Text != "" {
				acc.itemReasoning.WriteString(part.Text)
			}
		}
	case "function_call":
		acc.toolCalls = append(acc.toolCalls, ToolCall{
			ID:   item.CallID,
			Type: "function",
			Function: ToolCallFunction{
				Name:      item.Name,
				Arguments: item.Arguments,
			},
		})
	}
}

// codexResponsesSSEEvent is the subset of an SSE event payload we consume.
// Mirrors ResponsesStreamEvent in codex-rs/codex-api/src/sse/responses.rs:
// the "type" field names the event; response/item/delta carry the payloads.
type codexResponsesSSEEvent struct {
	Type  string              `json:"type"`
	Delta string              `json:"delta"`
	Item  *codexResponsesItem `json:"item"`
	Resp  *codexResponsesResp `json:"response"`
}

// codexResponsesResp is the "response" envelope carried by the lifecycle
// events (created/completed/failed/incomplete).
type codexResponsesResp struct {
	ID    string `json:"id"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// handleResponsesSSEEvent consumes one parsed SSE event and reports whether
// it was TERMINAL (completed/failed/incomplete). The stream MUST stop at the
// first terminal event — upstream's process_sse returns immediately after
// Completed (openai/codex#9170). A returned error is a semantic stream
// failure (response.failed / response.incomplete).
func handleResponsesSSEEvent(ev codexResponsesSSEEvent, acc *codexStreamAccumulator) (bool, error) {
	switch ev.Type {
	case "response.output_text.delta":
		acc.deltaContent.WriteString(ev.Delta)
	case "response.reasoning_summary_text.delta":
		acc.deltaReasoning.WriteString(ev.Delta)
	case "response.function_call_arguments.delta":
		// Arguments deltas key on item_id; nothing to fold here — the
		// complete function_call arrives via output_item.done below.
	case "response.output_item.added":
		// Start of an item; the authoritative content arrives at .done.
	case "response.output_item.done":
		if ev.Item != nil {
			parseResponsesItem(*ev.Item, acc)
		}
	case "response.created":
		// Lifecycle start only.
	case "response.completed":
		if ev.Resp != nil && ev.Resp.Usage != nil {
			acc.usage = TokenUsage{
				PromptTokens:     ev.Resp.Usage.InputTokens,
				CompletionTokens: ev.Resp.Usage.OutputTokens,
				TotalTokens:      ev.Resp.Usage.InputTokens + ev.Resp.Usage.OutputTokens,
			}
		}
		return true, nil
	case "response.failed":
		msg := "codex stream failed"
		if ev.Resp != nil && ev.Resp.Error != nil {
			msg = fmt.Sprintf("codex stream failed: %s", ev.Resp.Error.Message)
		}
		return true, &codexSSEError{Message: msg}
	case "response.incomplete":
		reason := "unknown"
		if ev.Resp != nil && ev.Resp.IncompleteDetails != nil {
			reason = ev.Resp.IncompleteDetails.Reason
		}
		return true, &codexSSEError{Message: fmt.Sprintf("codex stream incomplete: %s", reason)}
	default:
		// output_text.done, content_part.*, in_progress, response.metadata
		// and unknown *.delta kinds are explicitly unhandled upstream; ignore.
	}
	return false, nil
}

// codexSSEError marks a semantic stream failure so callers can distinguish it
// from transport errors when wrapping into ClientError.
type codexSSEError struct {
	Message string
}

func (e *codexSSEError) Error() string { return e.Message }

// parseResponsesSSE consumes an SSE body of Responses events, folding the
// deltas/items into a full Response. It is also the assembly path for
// non-callback callers of the streaming exchange: the accumulated Response
// is equivalent to what the non-streaming JSON parse produces (usage comes
// from response.completed, content/reasoning/tool calls from the folded
// items).
func (c *CodexClient) parseResponsesSSE(body io.Reader, cfg *ModelConfig, onDelta DeltaCallback) (*Response, error) {
	sc := newSSEScanner(body)
	acc := &codexStreamAccumulator{}
	sawTerminal := false

	for sc.Scan() {
		ev := sc.Event()
		if ev == nil || ev.Data == "" {
			continue
		}
		var parsed codexResponsesSSEEvent
		if err := json.Unmarshal([]byte(ev.Data), &parsed); err != nil {
			// Skip lines we can't parse rather than failing the whole
			// stream (same policy as client.go's OpenAI chunk parser).
			c.logger.Debug("codex: unparseable SSE event", "error", err, "data", ev.Data)
			continue
		}
		terminal, err := handleResponsesSSEEvent(parsed, acc)
		if err != nil {
			return nil, &ClientError{Message: err.Error(), Cause: err}
		}
		// Fire content deltas before the terminal break so the callback
		// sees every delta; an onDelta error aborts the stream.
		if onDelta != nil && parsed.Type == "response.output_text.delta" && parsed.Delta != "" {
			if err := onDelta(parsed.Delta); err != nil {
				return nil, &ClientError{Message: "stream aborted by delta callback", Cause: err}
			}
		}
		if terminal {
			sawTerminal = true
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, &ClientError{Message: "codex stream read failed", Cause: err}
	}
	if !sawTerminal {
		// Upstream parity: a stream that ends without response.completed is
		// an error, never a truncated success.
		return nil, &ClientError{Message: "codex stream closed before response.completed"}
	}

	// Prefer the authoritative item channel; fall back to the live delta
	// channel for deployments that stream deltas without output_item.done.
	content := acc.itemContent.String()
	if content == "" {
		content = acc.deltaContent.String()
	}
	reasoning := acc.itemReasoning.String()
	if reasoning == "" {
		reasoning = acc.deltaReasoning.String()
	}

	finish := "stop"
	if len(acc.toolCalls) > 0 {
		finish = "tool_calls"
	}
	return &Response{
		Content:      content,
		Reasoning:    reasoning,
		ToolCalls:    acc.toolCalls,
		Usage:        acc.usage,
		Model:        cfg.ModelID,
		FinishReason: finish,
	}, nil
}

// ChatWithDeltaCallback implements StreamingChatter for CodexClient. It
// POSTs stream:true with Accept: text/event-stream (the codex-rs wire
// contract) and invokes onDelta for each output_text delta. A non-nil
// onDelta error aborts the stream and is returned. nil onDelta falls back
// to plain Chat.
func (c *CodexClient) ChatWithDeltaCallback(ctx context.Context, messages []ChatMessage, onDelta DeltaCallback, opts ...ChatOption) (*Response, error) {
	if onDelta == nil {
		return c.Chat(ctx, messages, opts...)
	}

	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	chatOpts := &chatOptions{
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		topP:        cfg.TopP,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	payload := c.buildPayload(messages, cfg, chatOpts, true)
	resp, err := c.doRequest(ctx, payload, cfg, chatOpts.sessionID, onDelta)
	if err != nil {
		return nil, err
	}

	// Mirror Chat's budget accounting (mutex-free; Budget locks internally).
	if c.budget != nil {
		c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
		if cfg.CostPerMillionInput > 0 || cfg.CostPerMillionOutput > 0 {
			costUSD := float64(resp.Usage.PromptTokens)*cfg.CostPerMillionInput/1_000_000 +
				float64(resp.Usage.CompletionTokens)*cfg.CostPerMillionOutput/1_000_000
			if costUSD > 0 {
				c.budget.RecordCostWithScope(CostRecord{
					Timestamp:        time.Now(),
					CostUSD:          costUSD,
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
				}, chatOpts.taskID, chatOpts.sessionID)
			}
		}
	}
	return resp, nil
}

// Compile-time: CodexClient now satisfies StreamingChatter.
var _ StreamingChatter = (*CodexClient)(nil)
