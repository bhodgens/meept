package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// FuzzParseResponseWithTools exercises the chat-completion response parser
// (the HTTP-body stand-in path) with arbitrary content/reasoning/prefill
// bytes. Invariant: never panics; the parser either returns a valid Response
// or a non-nil error (ErrEmptyResponse included) — never both.
func FuzzParseResponseWithTools(f *testing.F) {
	seeds := []struct {
		content   string
		reasoning string
		prefill   string
		hasTools  bool
	}{
		{"", "", "", false},
		{"plain prose answer", "", "", false},
		{"\n\n", "", "", false},
		{"\n\nok", "", "", false},
		{`{"name": "write_file", "arguments": {"file_name": "hello.txt", "content": "hi"}}`, "", "pre", true},
		{"<|tool_call_start|>[write_file(file_name=\"a.txt\")]<|tool_call_end|>", "", "", true},
		{"", "thinking about it", "", false},
		{"", "<|tool_call_start|>[fn()]<|tool_call_end|>", "", true},
		{"[{\"type\": \"text\", \"text\": \"blocks\"}]", "", "", false},
		{"```json\nnull\n```", "", "", true},
		{"unicode \u4e2d\u6587 \u00e9 \U0001F600", "reasoning \u00e9", "prefill \u4e2d", true},
		{strings.Repeat("x", 10000), "", "", false},
		{`{"deeply": {"nested": {"json": [1,2,3]}}}`, "", "", true},
	}
	for _, s := range seeds {
		f.Add(s.content, s.reasoning, s.prefill, s.hasTools)
	}

	f.Fuzz(func(t *testing.T, content, reasoning, prefill string, hasTools bool) {
		c := &Client{config: &ModelConfig{ModelID: "fuzz-model"}, logger: discardLogger()}
		respMsg := ResponseMessage{Role: "assistant"}
		if content != "" {
			respMsg.Content = json.RawMessage(content)
		}
		respMsg.Reasoning = reasoning
		chatResp := &ChatResponse{
			Model:   "fuzz-model",
			Choices: []Choice{{FinishReason: "stop", Message: respMsg}},
		}
		got, err := c.parseResponseWithTools(chatResp, hasTools, "fuzz", "fuzz-model", prefill)
		if err != nil {
			// A parse refusal must not come with a response.
			if got != nil {
				t.Fatalf("parseResponseWithTools returned both response and error: %v", err)
			}
			return
		}
		if got == nil {
			t.Fatal("parseResponseWithTools returned nil response and nil error")
		}
	})
}

// FuzzSSEChunkParse exercises the streaming-path body parser: arbitrary
// bytes are fed through the same data:-line + json.Unmarshal shape the
// doStreamRequest scanner loop applies to an SSE HTTP body. Invariant:
// never panics on malformed or adversarial bodies.
func FuzzSSEChunkParse(f *testing.F) {
	seeds := []string{
		"",
		"data: [DONE]\n",
		"data: {}\n\n",
		"data: {\"choices\": []}\n\n",
		"data: {\"choices\": [{\"delta\": {\"content\": \"hi\"}}]}\n\n",
		"data: {\"choices\": [{\"delta\": {\"reasoning\": \"think\"}}], \"usage\": {\"prompt_tokens\": 1}}\n\n",
		"data: not json\n\n",
		"data: {\"choices\": [{\"delta\": {\"tool_calls\": [{\"index\": 0, \"id\": \"a\", \"function\": {\"name\": \"f\", \"arguments\": \"{}\"}}]}}]}\n\n",
		"event: x\ndata: y\n\n",
		"data:" + strings.Repeat(" {\"choices\":[", 200) + "\n",
		"data: {\"usage\": {\"completion_tokens_details\": {\"reasoning_tokens\": 3}}}\n\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// The per-line parse shape from doStreamRequest's scanner loop,
	// isolated from network I/O.
	//
	// DRIFT RISK (L15a): this is a HAND COPY of the production scanner, not
	// an extraction of it — extracting it would touch the quota-parked
	// streaming path in client.go, which is outside this fuzz target's
	// blast radius. The mirrored production function is
	// Client.doStreamRequest (internal/llm/client.go, the scanner.Text()
	// loop: CutPrefix "data:" → TrimPrefix " " → "[DONE]" break →
	// json.Unmarshal into the chunk struct). If you change that loop's line
	// handling, the delta/usage struct shape, or the tool_calls decoding,
	// UPDATE THIS COPY IN THE SAME COMMIT — otherwise the fuzzer keeps
	// proving properties about code the daemon no longer runs.
	parseSSEBody := func(body string) {
		for line := range strings.SplitSeq(body, "\n") {
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			data = strings.TrimPrefix(data, " ")
			if data == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content,omitempty"`
						Reasoning        string `json:"reasoning,omitempty"`
						Role             string `json:"role"`
						ToolCalls        []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Type     string `json:"type"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 {
				for _, tc := range chunk.Choices[0].Delta.ToolCalls {
					_ = tc
				}
				if chunk.Choices[0].FinishReason != nil {
					_ = *chunk.Choices[0].FinishReason
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, body string) {
		parseSSEBody(body)
	})
}
